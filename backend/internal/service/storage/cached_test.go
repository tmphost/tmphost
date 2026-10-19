// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package storage

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gatedStore holds a Put of a gated key until the test releases it, so
// tests can interleave writers deterministically.
type gatedStore struct {
	Service
	gated   string
	started chan struct{}
	release chan struct{}
}

func (s *gatedStore) Put(ctx context.Context, key string, data []byte) error {
	if key == s.gated {
		s.gated = ""
		close(s.started)
		<-s.release
	}
	return s.Service.Put(ctx, key, data)
}

func newCached(t *testing.T, cfg CacheConfig) (*cached, *local, string) {
	t.Helper()
	inner, root := newLocal(t)
	return NewCached(inner, cfg).(*cached), inner, root
}

func read(t *testing.T, s Service, key string) (string, error) {
	t.Helper()
	body, _, err := s.Get(context.Background(), key)
	if err != nil {
		return "", err
	}
	defer func() { _ = body.Close() }()
	b, err := io.ReadAll(body)
	require.NoError(t, err)
	return string(b), nil
}

func TestNewCached_Off(t *testing.T) {
	inner, _ := newLocal(t)
	assert.Same(t, Service(inner), NewCached(inner, CacheConfig{MaxBytes: 1}))
	assert.Same(t, Service(inner), NewCached(inner, CacheConfig{TTL: time.Minute}))
}

func TestCached_ServesFreshUploadsFromMemory(t *testing.T) {
	ctx := context.Background()
	s, inner, root := newCached(t, CacheConfig{TTL: time.Minute, MaxBytes: 1 << 20})

	require.NoError(t, s.Put(ctx, "u1/f1/a.txt", []byte("hello")))
	// The upload reached the inner storage too.
	got, err := read(t, inner, "u1/f1/a.txt")
	require.NoError(t, err)
	assert.Equal(t, "hello", got)

	// Gone from disk, still served from memory.
	require.NoError(t, os.RemoveAll(root))
	body, obj, err := s.Get(ctx, "u1/f1/a.txt")
	require.NoError(t, err)
	b, _ := io.ReadAll(body)
	assert.Equal(t, "hello", string(b))
	assert.Equal(t, "u1/f1/a.txt", obj.Key)
	assert.Equal(t, int64(5), obj.Size)
	assert.WithinDuration(t, time.Now(), obj.ModTime, time.Minute)

	// Anything not uploaded through the cache goes to the inner storage.
	_, err = read(t, s, "u1/f1/b.txt")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestCached_Expires(t *testing.T) {
	ctx := context.Background()
	s, inner, _ := newCached(t, CacheConfig{TTL: 50 * time.Millisecond, MaxBytes: 1 << 20})
	require.NoError(t, s.Put(ctx, "u1/f1/a.txt", []byte("hello")))
	require.NoError(t, inner.Delete(ctx, "u1/f1/a.txt"))

	require.Eventually(t, func() bool {
		_, err := read(t, s, "u1/f1/a.txt")
		return errors.Is(err, ErrNotFound)
	}, 5*time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool { return s.size.Load() == 0 }, 5*time.Second, 10*time.Millisecond)
}

func TestCached_ByteCap(t *testing.T) {
	ctx := context.Background()
	s, inner, _ := newCached(t, CacheConfig{TTL: time.Minute, MaxBytes: 10})

	require.NoError(t, s.Put(ctx, "u1/f1/a", []byte("aaaaaa")))
	require.NoError(t, s.Put(ctx, "u1/f1/b", []byte("bbbbbb")))
	// a was dropped to fit b.
	assert.Equal(t, []string{"u1/f1/b"}, s.lru.Keys())
	assert.Equal(t, int64(6), s.size.Load())

	// Replacing b counts its bytes once.
	require.NoError(t, s.Put(ctx, "u1/f1/b", []byte("bbbb")))
	assert.Equal(t, int64(4), s.size.Load())

	// Bigger than the whole cache: stored, but never kept in memory.
	require.NoError(t, s.Put(ctx, "u1/f1/c", []byte("ccccccccccc")))
	assert.Equal(t, []string{"u1/f1/b"}, s.lru.Keys())
	got, err := read(t, inner, "u1/f1/c")
	require.NoError(t, err)
	assert.Equal(t, "ccccccccccc", got)
}

func TestCached_PutError(t *testing.T) {
	ctx := context.Background()
	s, _, _ := newCached(t, CacheConfig{TTL: time.Minute, MaxBytes: 1 << 20})
	assert.ErrorIs(t, s.Put(ctx, "../a", []byte("x")), ErrInvalidKey)
	assert.Zero(t, s.lru.Len())
	assert.Empty(t, s.pending)
}

func TestCached_Delete(t *testing.T) {
	ctx := context.Background()
	s, _, _ := newCached(t, CacheConfig{TTL: time.Minute, MaxBytes: 1 << 20})
	require.NoError(t, s.Put(ctx, "u1/f1/a", []byte("a")))
	require.NoError(t, s.Put(ctx, "u1/f1/b", []byte("b")))
	require.NoError(t, s.Put(ctx, "u1/f2/c", []byte("c")))

	require.NoError(t, s.Delete(ctx, "u1/f1/a"))
	_, err := read(t, s, "u1/f1/a")
	assert.ErrorIs(t, err, ErrNotFound)

	require.NoError(t, s.DeletePrefix(ctx, "u1/f1/"))
	assert.Equal(t, []string{"u1/f2/c"}, s.lru.Keys())
	assert.Equal(t, int64(1), s.size.Load())

	assert.ErrorIs(t, s.Delete(ctx, "../a"), ErrInvalidKey)
	assert.ErrorIs(t, s.DeletePrefix(ctx, "u1"), ErrInvalidKey)
}

// gatedPut starts a Put of key that blocks in the inner storage until
// release is closed, and returns its result channel.
func gatedPut(t *testing.T, cfg CacheConfig, key string) (*cached, *gatedStore, chan error) {
	t.Helper()
	inner, _ := newLocal(t)
	g := &gatedStore{Service: inner, gated: key, started: make(chan struct{}), release: make(chan struct{})}
	s := NewCached(g, cfg).(*cached)
	done := make(chan error, 1)
	go func() { done <- s.Put(context.Background(), key, []byte("old")) }()
	<-g.started
	return s, g, done
}

func TestCached_SlowerOlderPutIsNotCached(t *testing.T) {
	ctx := context.Background()
	s, g, done := gatedPut(t, CacheConfig{TTL: time.Minute, MaxBytes: 1 << 20}, "u1/f1/a")

	require.NoError(t, s.Put(ctx, "u1/f1/a", []byte("new")))
	close(g.release)
	require.NoError(t, <-done)

	e, ok := s.lru.Peek("u1/f1/a")
	require.True(t, ok)
	assert.Equal(t, "new", string(e.data))
	assert.Empty(t, s.pending)
}

func TestCached_DeleteDuringPut(t *testing.T) {
	for name, del := range map[string]func(s *cached) error{
		"key":    func(s *cached) error { return s.Delete(context.Background(), "u1/f1/a") },
		"prefix": func(s *cached) error { return s.DeletePrefix(context.Background(), "u1/f1/") },
	} {
		t.Run(name, func(t *testing.T) {
			s, g, done := gatedPut(t, CacheConfig{TTL: time.Minute, MaxBytes: 1 << 20}, "u1/f1/a")
			require.NoError(t, del(s))
			close(g.release)
			require.NoError(t, <-done)
			assert.Zero(t, s.lru.Len())
		})
	}
}
