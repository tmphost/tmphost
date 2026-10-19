// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errPut = errors.New("put failed")

// fakeStore is a local store whose Puts can be held until the test lets
// them go, or made to fail, so tests interleave writers deterministically.
type fakeStore struct {
	*local
	mu    sync.Mutex
	holds map[string]*hold
	fails int
}

type hold struct {
	started, release chan struct{}
}

func (s *fakeStore) Put(ctx context.Context, key string, data []byte) error {
	s.mu.Lock()
	h := s.holds[key]
	delete(s.holds, key)
	fail := s.fails > 0
	if fail {
		s.fails--
	}
	s.mu.Unlock()
	if h != nil {
		close(h.started)
		<-h.release
	}
	if fail {
		return errPut
	}
	return s.local.Put(ctx, key, data)
}

// hold makes the next Put of key wait until the returned release is closed.
func (s *fakeStore) hold(key string) *hold {
	h := &hold{started: make(chan struct{}), release: make(chan struct{})}
	s.mu.Lock()
	s.holds[key] = h
	s.mu.Unlock()
	return h
}

func (s *fakeStore) failNext(n int) {
	s.mu.Lock()
	s.fails = n
	s.mu.Unlock()
}

func newCached(t *testing.T, cfg CacheConfig) (*cached, *fakeStore, string) {
	t.Helper()
	inner, root := newLocal(t)
	f := &fakeStore{local: inner, holds: map[string]*hold{}}
	c := NewCached(f, cfg).(*cached)
	c.retryDelay = time.Millisecond
	return c, f, root
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

func keys(objects []Object) []string {
	out := make([]string, len(objects))
	for i, o := range objects {
		out[i] = o.Key
	}
	return out
}

// done reports whether ch was closed or sent to by now.
func done[T any](ch <-chan T) bool {
	select {
	case <-ch:
		return true
	case <-time.After(20 * time.Millisecond):
		return false
	}
}

var (
	syncCache  = CacheConfig{TTL: time.Minute, MaxBytes: 1 << 20}
	asyncCache = CacheConfig{TTL: time.Minute, MaxBytes: 1 << 20, Async: true}
)

func TestNewCached_Off(t *testing.T) {
	inner, _ := newLocal(t)
	assert.Same(t, Service(inner), NewCached(inner, CacheConfig{MaxBytes: 1}))
	assert.Same(t, Service(inner), NewCached(inner, CacheConfig{TTL: time.Minute}))
	assert.NoError(t, Flush(context.Background(), inner))
}

func TestCached_ServesFreshUploadsFromMemory(t *testing.T) {
	ctx := context.Background()
	s, inner, root := newCached(t, syncCache)

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
	s, inner, _ := newCached(t, syncCache)
	assert.ErrorIs(t, s.Put(ctx, "../a", []byte("x")), ErrInvalidKey)

	// A synchronous write is tried once and its error returned.
	inner.failNext(1)
	assert.ErrorIs(t, s.Put(ctx, "u1/f1/a", []byte("x")), errPut)
	assert.Zero(t, s.lru.Len())
	assert.Empty(t, s.writes)
}

func TestCached_Delete(t *testing.T) {
	ctx := context.Background()
	s, _, _ := newCached(t, syncCache)
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

func TestCached_WritesOfAKeyRunInOrder(t *testing.T) {
	ctx := context.Background()
	s, inner, _ := newCached(t, syncCache)
	require.NoError(t, s.Put(ctx, "u1/f1/a", []byte("v0")))

	h := inner.hold("u1/f1/a")
	first := make(chan error, 1)
	go func() { first <- s.Put(ctx, "u1/f1/a", []byte("v1")) }()
	<-h.started
	// While v1 is being written, readers get the stored version.
	got, err := read(t, s, "u1/f1/a")
	require.NoError(t, err)
	assert.Equal(t, "v0", got)

	second := make(chan error, 1)
	go func() { second <- s.Put(ctx, "u1/f1/a", []byte("v2")) }()
	assert.False(t, done(second), "v2 waits for v1")
	close(h.release)
	require.NoError(t, <-first)
	require.NoError(t, <-second)

	e, ok := s.lru.Peek("u1/f1/a")
	require.True(t, ok)
	assert.Equal(t, "v2", string(e.data))
	got, err = read(t, inner, "u1/f1/a")
	require.NoError(t, err)
	assert.Equal(t, "v2", got)
	assert.Empty(t, s.writes)
}

func TestCached_AsyncPut(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s, inner, _ := newCached(t, asyncCache)
	require.NoError(t, s.Put(ctx, "u1/f1/a", []byte("old")))
	require.NoError(t, Flush(ctx, s))
	require.NoError(t, inner.Put(ctx, "u1/f1/b", []byte("b")))

	h := inner.hold("u1/f1/a")
	// Answered before the inner storage has it, and the request's context
	// ending doesn't stop the write.
	require.NoError(t, s.Put(ctx, "u1/f1/a", []byte("new!")))
	cancel()
	<-h.started

	got, err := read(t, s, "u1/f1/a")
	require.NoError(t, err)
	assert.Equal(t, "new!", got)
	objects, err := s.List(context.Background(), "u1/f1/")
	require.NoError(t, err)
	assert.Equal(t, []string{"u1/f1/a", "u1/f1/b"}, keys(objects))
	assert.Equal(t, int64(4), objects[0].Size, "the pending version")
	assert.Equal(t, int64(4), s.writing)

	flushCtx, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stop()
	assert.ErrorIs(t, Flush(flushCtx, s), context.DeadlineExceeded)

	close(h.release)
	require.NoError(t, Flush(context.Background(), s))
	got, err = read(t, inner, "u1/f1/a")
	require.NoError(t, err)
	assert.Equal(t, "new!", got)
	assert.Equal(t, []string{"u1/f1/a"}, s.lru.Keys())
	assert.Zero(t, s.writing)
	assert.Empty(t, s.writes)

	objects, err = s.List(context.Background(), "u1/f1/")
	require.NoError(t, err)
	assert.Equal(t, []string{"u1/f1/a", "u1/f1/b"}, keys(objects))
	_, err = s.List(context.Background(), "u1")
	assert.ErrorIs(t, err, ErrInvalidKey)
}

func TestCached_AsyncBudget(t *testing.T) {
	ctx := context.Background()
	s, inner, _ := newCached(t, CacheConfig{TTL: time.Minute, MaxBytes: 4, Async: true})

	h := inner.hold("u1/f1/a")
	require.NoError(t, s.Put(ctx, "u1/f1/a", []byte("aaa")))
	<-h.started
	// Over the budget of writes in flight: the caller waits for this one.
	inner.failNext(1)
	assert.ErrorIs(t, s.Put(ctx, "u1/f1/b", []byte("bbb")), errPut)
	close(h.release)
	require.NoError(t, Flush(ctx, s))
}

func TestCached_AsyncRetries(t *testing.T) {
	var logs bytes.Buffer
	saved := zlog.Logger
	zlog.Logger = zerolog.New(&logs)
	t.Cleanup(func() { zlog.Logger = saved })
	ctx := context.Background()
	s, inner, _ := newCached(t, asyncCache)

	inner.failNext(2)
	require.NoError(t, s.Put(ctx, "u1/f1/a", []byte("a")))
	require.NoError(t, Flush(ctx, s))
	got, err := read(t, inner, "u1/f1/a")
	require.NoError(t, err)
	assert.Equal(t, "a", got)
	assert.Empty(t, logs.String())

	inner.failNext(3)
	require.NoError(t, s.Put(ctx, "u1/f1/b", []byte("b")))
	require.NoError(t, Flush(ctx, s))
	_, err = read(t, s, "u1/f1/b")
	assert.ErrorIs(t, err, ErrNotFound, "given up: not served from memory")
	assert.Contains(t, logs.String(), "background write failed")
	assert.Contains(t, logs.String(), "u1/f1/b")
}

// signingStore is a fakeStore that signs URLs.
type signingStore struct{ *fakeStore }

func (s signingStore) SignGet(_ context.Context, key string, opts SignOptions) (string, error) {
	return "https://bucket/" + key + "?ttl=" + opts.TTL.String(), nil
}

func TestCached_SignGet(t *testing.T) {
	ctx := context.Background()
	s, _, _ := newCached(t, CacheConfig{TTL: time.Minute, MaxBytes: 1 << 20})
	// The inner storage doesn't sign: everything is read with Get.
	u, err := s.SignGet(ctx, "u1/f1/a", SignOptions{TTL: time.Minute})
	require.NoError(t, err)
	assert.Empty(t, u)

	s, inner, _ := newCached(t, asyncCache)
	s.Service = signingStore{inner}
	u, err = s.SignGet(ctx, "u1/f1/a", SignOptions{TTL: time.Minute})
	require.NoError(t, err)
	assert.Equal(t, "https://bucket/u1/f1/a?ttl=1m0s", u, "not in memory: signed by the bucket")

	// Still being written: only memory has it.
	h := inner.hold("u1/f1/a")
	require.NoError(t, s.Put(ctx, "u1/f1/a", []byte("new")))
	<-h.started
	u, err = s.SignGet(ctx, "u1/f1/a", SignOptions{TTL: time.Minute})
	require.NoError(t, err)
	assert.Empty(t, u)

	// Written and fresh: served from memory.
	close(h.release)
	require.NoError(t, Flush(ctx, s))
	u, err = s.SignGet(ctx, "u1/f1/a", SignOptions{TTL: time.Minute})
	require.NoError(t, err)
	assert.Empty(t, u)
}

func TestCached_DeleteWaitsForWrites(t *testing.T) {
	for name, del := range map[string]func(s *cached) error{
		"key":    func(s *cached) error { return s.Delete(context.Background(), "u1/f1/a") },
		"prefix": func(s *cached) error { return s.DeletePrefix(context.Background(), "u1/f1/") },
	} {
		t.Run(name, func(t *testing.T) {
			s, inner, _ := newCached(t, asyncCache)
			h := inner.hold("u1/f1/a")
			require.NoError(t, s.Put(context.Background(), "u1/f1/a", []byte("a")))
			<-h.started

			deleted := make(chan error, 1)
			go func() { deleted <- del(s) }()
			assert.False(t, done(deleted), "the delete waits for the write")
			close(h.release)
			require.NoError(t, <-deleted)

			_, err := read(t, s, "u1/f1/a")
			assert.ErrorIs(t, err, ErrNotFound)
			assert.Zero(t, s.lru.Len())
		})
	}
}
