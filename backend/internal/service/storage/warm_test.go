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
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errInner = errors.New("inner failed")

// hookStore is a local store that runs a hook before each call, so tests
// can fail a call or interleave another one with it.
type hookStore struct {
	*local
	beforePut    func(key string) error
	beforeGet    func(key string) error
	beforeDelete func()
	getBody      io.ReadCloser
}

func (s *hookStore) Put(ctx context.Context, key string, data []byte) error {
	if s.beforePut != nil {
		if err := s.beforePut(key); err != nil {
			return err
		}
	}
	return s.local.Put(ctx, key, data)
}

func (s *hookStore) Get(ctx context.Context, key string) (io.ReadCloser, Object, error) {
	if s.beforeGet != nil {
		if err := s.beforeGet(key); err != nil {
			return nil, Object{}, err
		}
	}
	if s.getBody != nil {
		return s.getBody, Object{Key: key, Size: 3}, nil
	}
	return s.local.Get(ctx, key)
}

func (s *hookStore) Delete(ctx context.Context, key string) error {
	if s.beforeDelete != nil {
		s.beforeDelete()
	}
	return s.local.Delete(ctx, key)
}

func (s *hookStore) DeletePrefix(ctx context.Context, prefix string) error {
	if s.beforeDelete != nil {
		s.beforeDelete()
	}
	return s.local.DeletePrefix(ctx, prefix)
}

// signerStore is a hookStore that can sign.
type signerStore struct{ *hookStore }

func (signerStore) SignGet(_ context.Context, key string, _ SignOptions) (string, error) {
	return "https://bucket/" + key, nil
}

func newWarm(t *testing.T, maxBytes int64) (*warm, *hookStore, string) {
	t.Helper()
	inner, _ := newLocal(t)
	h := &hookStore{local: inner}
	dir := filepath.Join(t.TempDir(), "warm")
	s, err := NewWarm(h, WarmConfig{Dir: dir, MaxBytes: maxBytes, Under: 8})
	require.NoError(t, err)
	return s.(*warm), h, dir
}

func readObj(t *testing.T, s Service, key string) (string, Object) {
	t.Helper()
	rc, obj, err := s.Get(context.Background(), key)
	require.NoError(t, err)
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(rc)
	require.NoError(t, err)
	return string(b), obj
}

func TestNewWarm_Off(t *testing.T) {
	inner, _ := newLocal(t)
	s, err := NewWarm(inner, WarmConfig{Dir: t.TempDir(), Under: 8})
	require.NoError(t, err)
	assert.Same(t, inner, s)
	s, err = NewWarm(inner, WarmConfig{Dir: t.TempDir(), MaxBytes: 8})
	require.NoError(t, err)
	assert.Same(t, inner, s)
}

func TestNewWarm_EmptiesDir(t *testing.T) {
	inner, _ := newLocal(t)
	dir := filepath.Join(t.TempDir(), "warm")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "u1"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "u1", "old"), []byte("old"), 0o600))
	_, err := NewWarm(inner, WarmConfig{Dir: dir, MaxBytes: 8, Under: 8})
	require.NoError(t, err)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestNewWarm_Errors(t *testing.T) {
	inner, _ := newLocal(t)
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	// A directory can't be made under a file.
	_, err := NewWarm(inner, WarmConfig{Dir: filepath.Join(file, "warm"), MaxBytes: 8, Under: 8})
	assert.Error(t, err)

	// Nor can the old copies be removed from a read-only directory.
	parent := filepath.Join(t.TempDir(), "ro")
	dir := filepath.Join(parent, "warm")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "x"), nil, 0o600))
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if os.Geteuid() == 0 {
		t.Skip("root ignores permissions")
	}
	_, err = NewWarm(inner, WarmConfig{Dir: dir, MaxBytes: 8, Under: 8})
	assert.Error(t, err)
	// Nothing to remove, but no directory can be made there either.
	_, err = NewWarm(inner, WarmConfig{Dir: filepath.Join(dir, "sub"), MaxBytes: 8, Under: 8})
	assert.Error(t, err)
}

func TestWarm_PutKeepsSmallFiles(t *testing.T) {
	w, h, dir := newWarm(t, 64)
	ctx := context.Background()
	require.NoError(t, w.Put(ctx, "u1/f1/small", []byte("tiny")))
	require.NoError(t, w.Put(ctx, "u1/f1/big", []byte("12345678")))
	assert.FileExists(t, filepath.Join(dir, "u1/f1/small"))
	assert.NoFileExists(t, filepath.Join(dir, "u1/f1/big"))

	// The inner copy is gone, but the small file is still read from disk.
	require.NoError(t, h.local.DeletePrefix(ctx, "u1/"))
	body, obj := readObj(t, w, "u1/f1/small")
	assert.Equal(t, "tiny", body)
	assert.Equal(t, int64(4), obj.Size)
	_, _, err := w.Get(ctx, "u1/f1/big")
	assert.ErrorIs(t, err, ErrNotFound)

	assert.ErrorIs(t, w.Put(ctx, "../x", []byte("x")), ErrInvalidKey)
}

func TestWarm_PutWritesDiskFirst(t *testing.T) {
	w, h, _ := newWarm(t, 64)
	ctx := context.Background()
	var during string
	h.beforePut = func(key string) error {
		during, _ = readObj(t, w, key)
		return nil
	}
	require.NoError(t, w.Put(ctx, "u1/f1/a", []byte("new")))
	assert.Equal(t, "new", during, "served from disk while the inner write runs")
}

func TestWarm_PutFailureDropsCopy(t *testing.T) {
	w, h, dir := newWarm(t, 64)
	ctx := context.Background()
	h.beforePut = func(string) error { return errInner }
	assert.ErrorIs(t, w.Put(ctx, "u1/f1/a", []byte("a")), errInner)
	assert.NoFileExists(t, filepath.Join(dir, "u1/f1/a"))
	assert.Zero(t, w.size)
	assert.Empty(t, w.owner)
}

func TestWarm_PutOverlappedByNewerWrite(t *testing.T) {
	w, h, _ := newWarm(t, 64)
	ctx := context.Background()
	first := true
	h.beforePut = func(key string) error {
		if first {
			first = false
			// A newer write lands while this one is still going, and this
			// one then fails: the newer copy must stay.
			require.NoError(t, w.Put(ctx, key, []byte("newer")))
			return errInner
		}
		return nil
	}
	assert.ErrorIs(t, w.Put(ctx, "u1/f1/a", []byte("older")), errInner)
	h.beforePut = nil
	require.NoError(t, h.local.DeletePrefix(ctx, "u1/"))
	body, _ := readObj(t, w, "u1/f1/a")
	assert.Equal(t, "newer", body)
	assert.Empty(t, w.owner)
}

func TestWarm_GetKeepsSmallFiles(t *testing.T) {
	w, h, dir := newWarm(t, 64)
	ctx := context.Background()
	require.NoError(t, h.local.Put(ctx, "u1/f1/small", []byte("tiny")))
	require.NoError(t, h.local.Put(ctx, "u1/f1/big", []byte("12345678")))

	body, _ := readObj(t, w, "u1/f1/small")
	assert.Equal(t, "tiny", body)
	body, _ = readObj(t, w, "u1/f1/big")
	assert.Equal(t, "12345678", body)
	assert.FileExists(t, filepath.Join(dir, "u1/f1/small"))
	assert.NoFileExists(t, filepath.Join(dir, "u1/f1/big"))

	require.NoError(t, h.local.DeletePrefix(ctx, "u1/"))
	body, _ = readObj(t, w, "u1/f1/small")
	assert.Equal(t, "tiny", body)

	_, _, err := w.Get(ctx, "u1/f1/missing")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestWarm_GetReadError(t *testing.T) {
	w, h, _ := newWarm(t, 64)
	h.getBody = io.NopCloser(io.MultiReader(bytes.NewReader([]byte("a")), errReader{}))
	_, _, err := w.Get(context.Background(), "u1/f1/a")
	assert.ErrorIs(t, err, errInner)
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errInner }

func TestWarm_GetHitWithFileGone(t *testing.T) {
	w, _, dir := newWarm(t, 64)
	ctx := context.Background()
	require.NoError(t, w.Put(ctx, "u1/f1/a", []byte("a")))
	require.NoError(t, os.Remove(filepath.Join(dir, "u1/f1/a")))
	body, _ := readObj(t, w, "u1/f1/a")
	assert.Equal(t, "a", body, "read from inner")
}

func TestWarm_GetSkipsCacheWhenWriteOverlaps(t *testing.T) {
	w, h, dir := newWarm(t, 64)
	ctx := context.Background()
	require.NoError(t, h.local.Put(ctx, "u1/f1/a", []byte("old")))
	h.beforeGet = func(key string) error {
		h.beforeGet = nil
		// A delete of another key runs while the read fetches.
		return w.Delete(ctx, "u1/f1/other")
	}
	body, _ := readObj(t, w, "u1/f1/a")
	assert.Equal(t, "old", body)
	assert.NoFileExists(t, filepath.Join(dir, "u1/f1/a"))
}

func TestWarm_Evicts(t *testing.T) {
	w, _, dir := newWarm(t, 10)
	ctx := context.Background()
	require.NoError(t, w.Put(ctx, "u1/f1/a", []byte("aaaa")))
	require.NoError(t, w.Put(ctx, "u1/f1/b", []byte("bbbb")))
	readObj(t, w, "u1/f1/a")
	require.NoError(t, w.Put(ctx, "u1/f1/c", []byte("cccc")))
	assert.FileExists(t, filepath.Join(dir, "u1/f1/a"))
	assert.NoFileExists(t, filepath.Join(dir, "u1/f1/b"), "least recently used")
	assert.FileExists(t, filepath.Join(dir, "u1/f1/c"))
	assert.Equal(t, int64(8), w.size)

	// Overwriting replaces the copy and its size.
	require.NoError(t, w.Put(ctx, "u1/f1/a", []byte("a")))
	assert.Equal(t, int64(5), w.size)
}

func TestWarm_SkipsFilesOverTheCap(t *testing.T) {
	w, _, dir := newWarm(t, 2)
	require.NoError(t, w.Put(context.Background(), "u1/f1/a", []byte("aaa")))
	assert.NoFileExists(t, filepath.Join(dir, "u1/f1/a"))
}

func TestWarm_DiskWriteFails(t *testing.T) {
	w, _, dir := newWarm(t, 64)
	ctx := context.Background()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "u1"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "u1", "f1"), nil, 0o600))
	// u1/f1 is a file on disk, so u1/f1/a can't be written there.
	require.NoError(t, w.Put(ctx, "u1/f1/a", []byte("a")))
	assert.False(t, w.lru.Contains("u1/f1/a"))
	body, _ := readObj(t, w, "u1/f1/a")
	assert.Equal(t, "a", body)
}

func TestWarm_SignGet(t *testing.T) {
	w, h, _ := newWarm(t, 64)
	ctx := context.Background()
	require.NoError(t, w.Put(ctx, "u1/f1/a", []byte("a")))
	u, err := w.SignGet(ctx, "u1/f1/b", SignOptions{TTL: time.Minute})
	require.NoError(t, err)
	assert.Empty(t, u, "inner can't sign")

	w.Service = signerStore{h}
	u, err = w.SignGet(ctx, "u1/f1/a", SignOptions{TTL: time.Minute})
	require.NoError(t, err)
	assert.Empty(t, u, "read from disk")
	u, err = w.SignGet(ctx, "u1/f1/b", SignOptions{TTL: time.Minute})
	require.NoError(t, err)
	assert.Equal(t, "https://bucket/u1/f1/b", u)
}

func TestWarm_Delete(t *testing.T) {
	w, _, dir := newWarm(t, 64)
	ctx := context.Background()
	require.NoError(t, w.Put(ctx, "u1/f1/a", []byte("a")))
	require.NoError(t, w.Delete(ctx, "u1/f1/a"))
	assert.NoFileExists(t, filepath.Join(dir, "u1/f1/a"))
	_, _, err := w.Get(ctx, "u1/f1/a")
	assert.ErrorIs(t, err, ErrNotFound)
	assert.Empty(t, w.owner)
}

func TestWarm_DeleteOverlapsPut(t *testing.T) {
	w, h, dir := newWarm(t, 64)
	ctx := context.Background()
	h.beforePut = func(key string) error {
		h.beforePut = nil
		require.NoError(t, w.Delete(ctx, key))
		return nil
	}
	require.NoError(t, w.Put(ctx, "u1/f1/a", []byte("a")))
	assert.NoFileExists(t, filepath.Join(dir, "u1/f1/a"), "the delete dropped it")
	assert.Empty(t, w.owner)

	// A write that starts while a delete of its key runs owns the key.
	h.beforeDelete = func() {
		h.beforeDelete = nil
		require.NoError(t, w.Put(ctx, "u1/f1/b", []byte("b")))
	}
	require.NoError(t, w.Delete(ctx, "u1/f1/b"))
	assert.Empty(t, w.owner)
}

func TestWarm_DeletePrefix(t *testing.T) {
	w, _, dir := newWarm(t, 64)
	ctx := context.Background()
	require.NoError(t, w.Put(ctx, "u1/f1/a", []byte("a")))
	require.NoError(t, w.Put(ctx, "u1/f2/a", []byte("a")))
	require.NoError(t, w.DeletePrefix(ctx, "u1/f1/"))
	assert.NoFileExists(t, filepath.Join(dir, "u1/f1/a"))
	assert.FileExists(t, filepath.Join(dir, "u1/f2/a"))
	assert.Empty(t, w.deleting)
}

func TestWarm_DeletePrefixOverlapsPut(t *testing.T) {
	w, h, dir := newWarm(t, 64)
	ctx := context.Background()
	h.beforePut = func(string) error {
		h.beforePut = nil
		require.NoError(t, w.DeletePrefix(ctx, "u1/"))
		return nil
	}
	require.NoError(t, w.Put(ctx, "u1/f1/a", []byte("a")))
	assert.NoFileExists(t, filepath.Join(dir, "u1/f1/a"))

	// Writes under a prefix being deleted aren't kept at all, even when two
	// deletes of it run at once.
	h.beforeDelete = func() {
		h.beforeDelete = nil
		require.NoError(t, w.DeletePrefix(ctx, "u1/"))
		require.NoError(t, w.Put(ctx, "u1/f1/b", []byte("b")))
		assert.NoFileExists(t, filepath.Join(dir, "u1/f1/b"))
		assert.Equal(t, 1, w.deleting["u1/"])
	}
	require.NoError(t, w.DeletePrefix(ctx, "u1/"))
	assert.Empty(t, w.deleting)
}

func TestWarm_PutRetries(t *testing.T) {
	w, h, dir := newWarm(t, 64)
	w.retries, w.retryDelay = 2, time.Millisecond
	ctx := context.Background()
	fails := 2
	h.beforePut = func(string) error {
		if fails > 0 {
			fails--
			return errInner
		}
		return nil
	}
	require.NoError(t, w.Put(ctx, "u1/f1/a", []byte("a")))
	assert.FileExists(t, filepath.Join(dir, "u1/f1/a"))
	body, err := os.ReadFile(filepath.Join(h.root, "u1/f1/a"))
	require.NoError(t, err)
	assert.Equal(t, "a", string(body))

	// Past the retries the copy goes.
	fails = 3
	assert.ErrorIs(t, w.Put(ctx, "u1/f1/b", []byte("b")), errInner)
	assert.NoFileExists(t, filepath.Join(dir, "u1/f1/b"))

	// A file not kept on disk is not retried.
	fails = 1
	assert.ErrorIs(t, w.Put(ctx, "u1/f1/big", []byte("12345678")), errInner)
	assert.Zero(t, fails)
}

func TestWarm_PutRetryCancelled(t *testing.T) {
	w, h, dir := newWarm(t, 64)
	w.retries, w.retryDelay = 5, time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	h.beforePut = func(string) error {
		cancel()
		return errInner
	}
	assert.ErrorIs(t, w.Put(ctx, "u1/f1/a", []byte("a")), errInner)
	assert.NoFileExists(t, filepath.Join(dir, "u1/f1/a"))
}
