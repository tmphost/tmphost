// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package storage

import (
	"bytes"
	"context"
	"io"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	zlog "github.com/rs/zerolog/log"
)

// A background write is tried this many times, waiting _retryDelay, then
// twice that, and so on, between tries.
const (
	_asyncAttempts = 3
	_retryDelay    = time.Second
)

type (
	// CacheConfig sizes the in-memory cache of fresh uploads.
	CacheConfig struct {
		// TTL is how long an upload stays in memory after it is written.
		TTL time.Duration
		// MaxBytes caps the bytes held in memory; past it the least
		// recently used uploads are dropped first. Uploads still being
		// written in the background have a budget of the same size.
		MaxBytes int64
		// Async acknowledges a Put once the data is in memory and writes
		// it to the inner storage in the background.
		Async bool
	}

	cached struct {
		Service
		maxBytes   int64
		async      bool
		retryDelay time.Duration
		lru        *expirable.LRU[string, cacheEntry]
		size       atomic.Int64

		// mu guards writes, writing and the cache's writes.
		mu sync.Mutex
		// writes holds the newest write of each key still in flight. Each
		// write waits for the one before it on the same key, so the inner
		// storage always ends up with the newest data.
		writes map[string]*write
		// writing counts the bytes of background writes in flight.
		writing    int64
		background sync.WaitGroup
	}

	cacheEntry struct {
		data []byte
		obj  Object
	}

	write struct {
		cacheEntry
		prev *write
		// background is set for a write the caller does not wait for;
		// until it is done, Get and List serve it from memory.
		background bool
		done       chan struct{}
	}
)

// NewCached wraps inner so that each upload is also kept in memory for
// cfg.TTL after it is written, and served from there while it is fresh.
// Reads of anything else go to inner and are not cached. A zero TTL or byte
// cap turns the cache off and returns inner as it is.
func NewCached(inner Service, cfg CacheConfig) Service {
	if cfg.TTL <= 0 || cfg.MaxBytes <= 0 {
		return inner
	}
	c := &cached{Service: inner, maxBytes: cfg.MaxBytes, async: cfg.Async, retryDelay: _retryDelay, writes: map[string]*write{}}
	c.lru = expirable.NewLRU(0, c.evicted, cfg.TTL)
	return c
}

// Flush waits until every background write of s has finished, or ctx is
// done.
func Flush(ctx context.Context, s Service) error {
	c, ok := s.(*cached)
	if !ok {
		return nil
	}
	done := make(chan struct{})
	go func() {
		c.background.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// evicted runs under the LRU's own lock, so it only touches the counter.
func (c *cached) evicted(_ string, e cacheEntry) {
	c.size.Add(-int64(len(e.data)))
}

func (c *cached) Put(ctx context.Context, key string, data []byte) error {
	// A background write has no caller left to tell about a bad key.
	if !validKey(key) {
		return ErrInvalidKey
	}
	n := int64(len(data))
	c.mu.Lock()
	w := &write{
		cacheEntry: cacheEntry{data: data, obj: Object{Key: key, Size: n, ModTime: time.Now()}},
		prev:       c.writes[key],
		// Past the budget, the caller waits: a slow store can't fill memory.
		background: c.async && c.writing+n <= c.maxBytes,
		done:       make(chan struct{}),
	}
	c.writes[key] = w
	c.lru.Remove(key)
	if w.background {
		c.writing += n
		c.background.Add(1)
	}
	c.mu.Unlock()

	if !w.background {
		return c.run(ctx, w)
	}
	go func() {
		defer c.background.Done()
		// The request is over by now, so its context must not cancel this.
		if err := c.run(context.WithoutCancel(ctx), w); err != nil {
			zlog.Error().Err(err).Str("key", key).Msg("storage: background write failed")
		}
	}()
	return nil
}

// run writes w to the inner storage once the write before it is done, then
// caches it if no newer write of its key came in meanwhile.
func (c *cached) run(ctx context.Context, w *write) error {
	if w.prev != nil {
		<-w.prev.done
		w.prev = nil
	}
	err := c.put(ctx, w)

	c.mu.Lock()
	key, n := w.obj.Key, int64(len(w.data))
	newest := c.writes[key] == w
	if newest {
		delete(c.writes, key)
	}
	if w.background {
		c.writing -= n
	}
	if err == nil && newest && n <= c.maxBytes {
		c.lru.Add(key, w.cacheEntry)
		c.size.Add(n)
		for c.size.Load() > c.maxBytes {
			c.lru.RemoveOldest()
		}
	}
	c.mu.Unlock()
	close(w.done)
	return err
}

// put writes w, retrying a background write before giving up on it.
func (c *cached) put(ctx context.Context, w *write) error {
	attempts := 1
	if w.background {
		attempts = _asyncAttempts
	}
	delay := c.retryDelay
	var err error
	for i := range attempts {
		if i > 0 {
			time.Sleep(delay)
			delay *= 2
		}
		if err = c.Service.Put(ctx, w.obj.Key, w.data); err == nil {
			return nil
		}
	}
	return err
}

func (c *cached) Get(ctx context.Context, key string) (io.ReadCloser, Object, error) {
	c.mu.Lock()
	w := c.writes[key]
	c.mu.Unlock()
	if w != nil && w.background {
		return io.NopCloser(bytes.NewReader(w.data)), w.obj, nil
	}
	if e, ok := c.lru.Get(key); ok {
		return io.NopCloser(bytes.NewReader(e.data)), e.obj, nil
	}
	return c.Service.Get(ctx, key)
}

// SignGet leaves what is in memory to Get: a background write isn't in
// the inner storage yet, and a fresh upload is served from memory anyway.
// Anything else is signed by the inner storage, when it can sign.
func (c *cached) SignGet(ctx context.Context, key string, opts SignOptions) (string, error) {
	c.mu.Lock()
	w := c.writes[key]
	c.mu.Unlock()
	if (w != nil && w.background) || c.lru.Contains(key) {
		return "", nil
	}
	s, ok := c.Service.(Signer)
	if !ok {
		return "", nil
	}
	return s.SignGet(ctx, key, opts)
}

// List adds the background writes under prefix to what the inner storage
// lists, so a folder shows its files before they are written.
func (c *cached) List(ctx context.Context, prefix string) ([]Object, error) {
	objects, err := c.Service.List(ctx, prefix)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	pending := map[string]Object{}
	for key, w := range c.writes {
		if w.background && strings.HasPrefix(key, prefix) {
			pending[key] = w.obj
		}
	}
	if len(pending) == 0 {
		return objects, nil
	}
	out := make([]Object, 0, len(objects)+len(pending))
	for _, o := range objects {
		if _, ok := pending[o.Key]; !ok {
			out = append(out, o)
		}
	}
	for _, o := range pending {
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// Delete waits for the writes of key in flight, so none of them can land
// after it.
func (c *cached) Delete(ctx context.Context, key string) error {
	c.wait(func(k string) bool { return k == key })
	err := c.Service.Delete(ctx, key)
	c.lru.Remove(key)
	return err
}

func (c *cached) DeletePrefix(ctx context.Context, prefix string) error {
	under := func(k string) bool { return strings.HasPrefix(k, prefix) }
	c.wait(under)
	err := c.Service.DeletePrefix(ctx, prefix)
	for _, key := range c.lru.Keys() {
		if under(key) {
			c.lru.Remove(key)
		}
	}
	return err
}

// wait blocks until the writes in flight of every key that match picks
// are done. The newest write of a key is done only after all older ones.
func (c *cached) wait(match func(string) bool) {
	c.mu.Lock()
	var ws []*write
	for key, w := range c.writes {
		if match(key) {
			ws = append(ws, w)
		}
	}
	c.mu.Unlock()
	for _, w := range ws {
		<-w.done
	}
}
