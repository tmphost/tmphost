// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tmphost/tmphost/backend/internal/data/model"
)

// countingFolders counts the lookups that reach the database and runs
// during on each, while the cache waits for it.
type countingFolders struct {
	FolderRepository
	reads  map[int64]int
	during func()
}

func (r *countingFolders) FindFolderByID(ctx context.Context, id int64) (model.Folder, error) {
	r.reads[id]++
	if r.during != nil {
		r.during()
	}
	return r.FolderRepository.FindFolderByID(ctx, id)
}

type folderCacheFixture struct {
	db    *countingFolders
	cache *cachedFolders
	clock time.Time
}

func newFolderCache(t *testing.T, maxBytes int64, folders ...model.Folder) *folderCacheFixture {
	t.Helper()
	x := &folderCacheFixture{
		db:    &countingFolders{FolderRepository: NewFolderRepository(newDB(t)), reads: map[int64]int{}},
		clock: time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC),
	}
	x.cache = NewCachedFolderRepository(x.db, maxBytes).(*cachedFolders)
	x.cache.now = func() time.Time { return x.clock }
	for _, f := range folders {
		_, err := x.db.CreateFolder(context.Background(), f)
		require.NoError(t, err)
	}
	return x
}

func (x *folderCacheFixture) find(t *testing.T, id int64) model.Folder {
	t.Helper()
	f, err := x.cache.FindFolderByID(context.Background(), id)
	require.NoError(t, err)
	return f
}

func TestNewCachedFolderRepository_Off(t *testing.T) {
	inner := NewFolderRepository(newDB(t))
	assert.Same(t, inner, NewCachedFolderRepository(inner, 0))
}

func TestCachedFolders_FindFolderByID(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	x := newFolderCache(t, 64<<20,
		model.Folder{ID: 1, UserID: 7, ExpiresAt: now.Add(time.Hour), Title: "a"},
		model.Folder{ID: 2, UserID: 7, ExpiresAt: now.Add(-time.Minute)},
	)

	assert.Equal(t, "a", x.find(t, 1).Title)
	assert.Equal(t, "a", x.find(t, 1).Title)
	assert.Equal(t, 1, x.db.reads[1], "the second lookup is served from memory")
	assert.Positive(t, x.cache.size)

	_, err := x.cache.FindFolderByID(ctx, 99)
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = x.cache.FindFolderByID(ctx, 99)
	assert.ErrorIs(t, err, ErrNotFound)
	assert.Equal(t, 2, x.db.reads[99], "misses are not cached")

	x.find(t, 2)
	x.find(t, 2)
	assert.Equal(t, 2, x.db.reads[2], "expired folders are not cached")

	// At its expiry the folder is dropped and read again.
	x.clock = now.Add(time.Hour)
	x.find(t, 1)
	assert.Equal(t, 2, x.db.reads[1])
	assert.Zero(t, x.cache.size)
	assert.Zero(t, x.cache.lru.Len())
}

func TestCachedFolders_FilesAreCopied(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	x := newFolderCache(t, 64<<20, model.Folder{ID: 1, UserID: 7, ExpiresAt: now.Add(time.Hour), Files: model.FolderFiles{}})
	require.NoError(t, x.db.SaveFiles(ctx, 1, "cli", model.FolderFiles{"a.txt": {Size: 3, ContentType: "text/plain"}}))

	f := x.find(t, 1)
	f.Files["b.txt"] = model.FolderFile{}
	f = x.find(t, 1)
	f.Files["c.txt"] = model.FolderFile{}
	assert.Equal(t, []string{"a.txt"}, mapKeys(x.find(t, 1).Files))
	assert.Equal(t, 1, x.db.reads[1])
}

func mapKeys(m model.FolderFiles) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestCachedFolders_WritesDrop(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	x := newFolderCache(t, 64<<20, model.Folder{ID: 1, UserID: 7, ExpiresAt: now.Add(time.Hour), Files: model.FolderFiles{}})
	reads := 1
	x.find(t, 1)

	for _, write := range []struct {
		name  string
		run   func() error
		check func(f model.Folder)
	}{
		{"title", func() error { return x.cache.UpdateFolderTitle(ctx, 1, "b") }, func(f model.Folder) {
			assert.Equal(t, "b", f.Title)
		}},
		{"extend", func() error { return x.cache.ExtendFolder(ctx, 1, now, now.Add(2*time.Hour)) }, func(f model.Folder) {
			assert.True(t, now.Add(2*time.Hour).Equal(f.ExpiresAt))
		}},
		{"save files", func() error {
			return x.cache.SaveFiles(ctx, 1, "cli", model.FolderFiles{"a.txt": {Size: 3}})
		}, func(f model.Folder) {
			assert.Contains(t, f.Files, "a.txt")
		}},
		{"delete file", func() error { return x.cache.DeleteFile(ctx, 1, "a.txt") }, func(f model.Folder) {
			assert.NotContains(t, f.Files, "a.txt")
		}},
	} {
		require.NoError(t, write.run(), write.name)
		assert.Zero(t, x.cache.lru.Len(), write.name)
		write.check(x.find(t, 1))
		reads++
		assert.Equal(t, reads, x.db.reads[1], write.name)
		x.find(t, 1)
		assert.Equal(t, reads, x.db.reads[1], write.name+": cached again")
	}

	require.NoError(t, x.cache.DeleteFolder(ctx, 1))
	_, err := x.cache.FindFolderByID(ctx, 1)
	assert.ErrorIs(t, err, ErrNotFound)
	assert.Zero(t, x.cache.size)
}

func TestCachedFolders_WriteDuringLookup(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	x := newFolderCache(t, 64<<20, model.Folder{ID: 1, UserID: 7, ExpiresAt: now.Add(time.Hour)})

	// The title changes after the lookup started reading.
	x.db.during = func() {
		x.db.during = nil
		require.NoError(t, x.cache.UpdateFolderTitle(ctx, 1, "b"))
	}
	x.find(t, 1)
	assert.Zero(t, x.cache.lru.Len(), "what the lookup read may be stale")
	assert.Equal(t, "b", x.find(t, 1).Title)
	assert.Equal(t, 1, x.cache.lru.Len())
}

func TestCachedFolders_ByteCap(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	f1 := model.Folder{ID: 1, UserID: 7, ExpiresAt: now.Add(time.Hour)}
	f2 := model.Folder{ID: 2, UserID: 7, ExpiresAt: now.Add(time.Hour)}
	f3 := model.Folder{ID: 3, UserID: 7, ExpiresAt: now.Add(time.Hour), Title: "a much longer title than the others"}
	x := newFolderCache(t, 2*folderBytes(f1), f1, f2, f3)

	x.find(t, 1)
	x.find(t, 2)
	x.find(t, 1) // 2 is now the least recently used
	assert.Equal(t, 2*folderBytes(f1), x.cache.size)
	x.find(t, 3)
	assert.Equal(t, folderBytes(f3), x.cache.size, "3 only fits alone")
	assert.Equal(t, []int64{3}, x.cache.lru.Keys())

	x.find(t, 1)
	assert.Equal(t, []int64{1}, x.cache.lru.Keys())

	huge := model.Folder{ID: 4, UserID: 7, ExpiresAt: now.Add(time.Hour), Files: model.FolderFiles{}}
	for _, p := range []string{"a", "b", "c", "d"} {
		huge.Files[p] = model.FolderFile{ContentType: "text/plain"}
	}
	assert.Greater(t, folderBytes(huge), x.cache.maxBytes)
	x.cache.store(x.cache.gen, huge)
	assert.Equal(t, []int64{1}, x.cache.lru.Keys(), "too big to cache")
}
