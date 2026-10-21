// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package storage

import (
	"bytes"
	"context"
	"io"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/golang-lru/v2/simplelru"
	zlog "github.com/rs/zerolog/log"
)

type (
	// WarmConfig sizes the local disk cache of small files.
	WarmConfig struct {
		// Dir holds the copies. It is emptied when the cache opens, since
		// the index of what it holds lives in memory.
		Dir string
		// MaxBytes caps the bytes kept on disk; past it the least recently
		// used files are dropped first.
		MaxBytes int64
		// Under is the size limit: only files smaller than it are kept.
		Under int64
		// Retries is how many more times a failed inner write of a kept
		// file is tried, waiting a second, then twice as long each time.
		// The disk copy is served meanwhile.
		Retries int
	}

	warm struct {
		Service
		disk       *local
		maxBytes   int64
		under      int64
		retries    int
		retryDelay time.Duration

		// mu guards everything below, and the files under disk.
		mu   sync.Mutex
		lru  *simplelru.LRU[string, Object]
		size int64
		// seq counts writes as they start and finish. A read caches what
		// it fetched only if seq did not move meanwhile.
		seq uint64
		// owner holds, per key with writes in flight, the seq of the
		// newest one; only that write may cache the key.
		owner map[string]uint64
		// deleting holds the prefixes being deleted; nothing under them
		// is cached until they are gone.
		deleting map[string]int
		// cuts counts prefix deletes; a write that overlapped one does not
		// cache, since the delete may have landed after it.
		cuts uint64
	}
)

// NewWarm wraps inner so that files smaller than cfg.Under are also kept on
// the local disk, and read from there instead of from inner. A file is
// kept when it is written, or the first time it is read. It stays until the
// byte cap pushes it out or it is overwritten or deleted. A zero byte cap
// turns the cache off and returns inner as it is.
func NewWarm(inner Service, cfg WarmConfig) (Service, error) {
	if cfg.MaxBytes <= 0 || cfg.Under <= 0 {
		return inner, nil
	}
	if err := os.RemoveAll(cfg.Dir); err != nil {
		return nil, err
	}
	disk, err := NewLocal(cfg.Dir)
	if err != nil {
		return nil, err
	}
	w := &warm{
		Service: inner, disk: disk.(*local), maxBytes: cfg.MaxBytes, under: cfg.Under,
		retries: cfg.Retries, retryDelay: _retryDelay,
		owner: map[string]uint64{}, deleting: map[string]int{},
	}
	// The byte cap bounds the cache, not the entry count. NewLRU only
	// fails on a size below 1.
	w.lru, _ = simplelru.NewLRU(math.MaxInt, w.evicted)
	return w, nil
}

// evicted runs under mu, when an entry is dropped for any reason.
func (w *warm) evicted(key string, o Object) {
	w.size -= o.Size
	_ = w.disk.Delete(context.Background(), key)
}

// Put writes a small file to disk first, so it is served from there while
// the inner write is still going, then to inner. If the inner write fails,
// or a prefix delete overlapped it, the disk copy is dropped again.
func (w *warm) Put(ctx context.Context, key string, data []byte) error {
	if !validKey(key) {
		return ErrInvalidKey
	}
	w.mu.Lock()
	w.seq++
	mine := w.seq
	w.owner[key] = mine
	cuts := w.cuts
	w.lru.Remove(key)
	kept := w.keep(key, data, Object{Key: key, Size: int64(len(data)), ModTime: time.Now()})
	w.mu.Unlock()

	err := w.put(ctx, key, data, kept)

	w.mu.Lock()
	defer w.mu.Unlock()
	w.seq++
	if w.owner[key] != mine {
		// A newer write or a delete of key took over, and dropped or
		// replaced this copy already.
		return err
	}
	delete(w.owner, key)
	if err != nil || w.cuts != cuts {
		w.lru.Remove(key)
	}
	return err
}

func (w *warm) Get(ctx context.Context, key string) (io.ReadCloser, Object, error) {
	w.mu.Lock()
	obj, hit := w.lru.Get(key)
	start := w.seq
	w.mu.Unlock()
	if hit {
		// A write or delete may have dropped the file since; then read
		// it from inner like any other.
		if f, _, err := w.disk.Get(ctx, key); err == nil {
			return f, obj, nil
		}
	}

	rc, obj, err := w.Service.Get(ctx, key)
	if err != nil || obj.Size >= w.under {
		return rc, obj, err
	}
	data, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		return nil, Object{}, err
	}
	w.mu.Lock()
	if w.seq == start {
		w.keep(key, data, obj)
	}
	w.mu.Unlock()
	return io.NopCloser(bytes.NewReader(data)), obj, nil
}

// put writes to inner, retrying a kept file with exponential backoff until
// it lands, the retries run out or ctx is done.
func (w *warm) put(ctx context.Context, key string, data []byte, kept bool) error {
	err := w.Service.Put(ctx, key, data)
	if !kept {
		return err
	}
	delay := w.retryDelay
	for i := 0; err != nil && i < w.retries; i++ {
		zlog.Warn().Err(err).Str("key", key).Int("retry", i+1).Msg("storage: write failed, retrying")
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return err
		}
		delay *= 2
		err = w.Service.Put(ctx, key, data)
	}
	return err
}

// keep writes data to disk and indexes it, unless it is too big or its key
// is being deleted, and reports whether it did. It must be called with mu
// held.
func (w *warm) keep(key string, data []byte, obj Object) bool {
	n := int64(len(data))
	if n >= w.under || n > w.maxBytes {
		return false
	}
	for prefix := range w.deleting {
		if strings.HasPrefix(key, prefix) {
			return false
		}
	}
	if err := w.disk.Put(context.Background(), key, data); err != nil {
		zlog.Warn().Err(err).Str("key", key).Msg("storage: warm cache write failed")
		return false
	}
	obj.Size = n
	w.lru.Add(key, obj)
	w.size += n
	for w.size > w.maxBytes {
		w.lru.RemoveOldest()
	}
	return true
}

// SignGet leaves a file on disk to Get, which reads it from there. Anything
// else is signed by the inner storage, when it can sign.
func (w *warm) SignGet(ctx context.Context, key string, opts SignOptions) (string, error) {
	w.mu.Lock()
	hit := w.lru.Contains(key)
	w.mu.Unlock()
	if hit {
		return "", nil
	}
	s, ok := w.Service.(Signer)
	if !ok {
		return "", nil
	}
	return s.SignGet(ctx, key, opts)
}

func (w *warm) Delete(ctx context.Context, key string) error {
	w.mu.Lock()
	w.seq++
	mine := w.seq
	w.owner[key] = mine
	w.lru.Remove(key)
	w.mu.Unlock()

	err := w.Service.Delete(ctx, key)

	w.mu.Lock()
	w.seq++
	if w.owner[key] == mine {
		delete(w.owner, key)
	}
	w.mu.Unlock()
	return err
}

func (w *warm) DeletePrefix(ctx context.Context, prefix string) error {
	w.mu.Lock()
	w.seq++
	w.cuts++
	w.deleting[prefix]++
	for _, key := range w.lru.Keys() {
		if strings.HasPrefix(key, prefix) {
			w.lru.Remove(key)
		}
	}
	w.mu.Unlock()

	err := w.Service.DeletePrefix(ctx, prefix)

	w.mu.Lock()
	w.seq++
	if w.deleting[prefix]--; w.deleting[prefix] == 0 {
		delete(w.deleting, prefix)
	}
	w.mu.Unlock()
	return err
}
