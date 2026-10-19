// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package storage

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
)

type (
	// CacheConfig sizes the in-memory cache of fresh uploads.
	CacheConfig struct {
		// TTL is how long an upload stays in memory after it is written.
		TTL time.Duration
		// MaxBytes caps the bytes held in memory; past it the least
		// recently used uploads are dropped first.
		MaxBytes int64
	}

	cached struct {
		Service
		maxBytes int64
		lru      *expirable.LRU[string, cacheEntry]
		size     atomic.Int64

		// mu guards the cache's writes and pending.
		mu sync.Mutex
		// pending holds the sequence number of the newest Put of each key
		// still writing to the inner storage. A Put caches its data only if
		// it is still the newest one for its key and no Delete removed the
		// key meanwhile, so a slower writer never caches stale bytes.
		pending map[string]uint64
		seq     uint64
	}

	cacheEntry struct {
		data []byte
		obj  Object
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
	c := &cached{Service: inner, maxBytes: cfg.MaxBytes, pending: map[string]uint64{}}
	c.lru = expirable.NewLRU(0, c.evicted, cfg.TTL)
	return c
}

// evicted runs under the LRU's own lock, so it only touches the counter.
func (c *cached) evicted(_ string, e cacheEntry) {
	c.size.Add(-int64(len(e.data)))
}

func (c *cached) Put(ctx context.Context, key string, data []byte) error {
	c.mu.Lock()
	c.seq++
	seq := c.seq
	c.pending[key] = seq
	c.lru.Remove(key)
	c.mu.Unlock()

	err := c.Service.Put(ctx, key, data)

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pending[key] != seq {
		return err
	}
	delete(c.pending, key)
	if err != nil || int64(len(data)) > c.maxBytes {
		return err
	}
	c.lru.Add(key, cacheEntry{data: data, obj: Object{Key: key, Size: int64(len(data)), ModTime: time.Now()}})
	c.size.Add(int64(len(data)))
	for c.size.Load() > c.maxBytes {
		c.lru.RemoveOldest()
	}
	return nil
}

func (c *cached) Get(ctx context.Context, key string) (io.ReadCloser, Object, error) {
	if e, ok := c.lru.Get(key); ok {
		return io.NopCloser(bytes.NewReader(e.data)), e.obj, nil
	}
	return c.Service.Get(ctx, key)
}

func (c *cached) Delete(ctx context.Context, key string) error {
	err := c.Service.Delete(ctx, key)
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.pending, key)
	c.lru.Remove(key)
	return err
}

func (c *cached) DeletePrefix(ctx context.Context, prefix string) error {
	err := c.Service.DeletePrefix(ctx, prefix)
	c.mu.Lock()
	defer c.mu.Unlock()
	for key := range c.pending {
		if strings.HasPrefix(key, prefix) {
			delete(c.pending, key)
		}
	}
	for _, key := range c.lru.Keys() {
		if strings.HasPrefix(key, prefix) {
			c.lru.Remove(key)
		}
	}
	return err
}
