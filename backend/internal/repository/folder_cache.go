// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package repository

import (
	"context"
	"maps"
	"math"
	"sync"
	"time"
	"unsafe"

	"github.com/hashicorp/golang-lru/v2/simplelru"

	"github.com/tmphost/tmphost/backend/internal/data/model"
)

const (
	// _folderEntryOverhead approximates what the LRU spends per entry
	// besides the folder itself: its list element and map slot.
	_folderEntryOverhead = 128
	// _folderFileOverhead approximates a Files map slot besides the path
	// and content type.
	_folderFileOverhead = 64
)

type cachedFolders struct {
	FolderRepository
	maxBytes int64

	// mu guards lru, size and gen.
	mu   sync.Mutex
	lru  *simplelru.LRU[int64, model.Folder]
	size int64
	// gen counts writes. A lookup caches what it read only when no write
	// happened while it read, so a write racing it can't leave a stale
	// entry behind.
	gen uint64
}

// NewCachedFolderRepository wraps inner so that folder lookups by ID are
// served from memory, up to maxBytes in all (least recently used dropped
// first). A folder stays cached until the byte cap pushes it out or a write
// to it (deleting it included) drops it; the next lookup reads it again.
// Its expiry doesn't drop it: callers check ExpiresAt themselves. A zero
// maxBytes returns inner as it is.
func NewCachedFolderRepository(inner FolderRepository, maxBytes int64) FolderRepository {
	if maxBytes <= 0 {
		return inner
	}
	c := &cachedFolders{FolderRepository: inner, maxBytes: maxBytes}
	// The byte cap bounds the cache, not the entry count. NewLRU only
	// fails on a size below 1.
	c.lru, _ = simplelru.NewLRU(math.MaxInt, c.evicted)
	return c
}

// folderBytes estimates the memory a cached folder takes.
func folderBytes(f model.Folder) int64 {
	n := int64(unsafe.Sizeof(f)) + int64(len(f.Title)+len(f.Agent)) + _folderEntryOverhead
	for p, m := range f.Files {
		n += int64(unsafe.Sizeof(m)) + int64(len(p)+len(m.ContentType)) + _folderFileOverhead
	}
	return n
}

// clone copies f's Files, so callers can't change a cached folder.
func clone(f model.Folder) model.Folder {
	f.Files = maps.Clone(f.Files)
	return f
}

// evicted runs under mu, from the LRU's Remove and RemoveOldest.
func (c *cachedFolders) evicted(_ int64, f model.Folder) {
	c.size -= folderBytes(f)
}

func (c *cachedFolders) FindFolderByID(ctx context.Context, id int64) (model.Folder, error) {
	c.mu.Lock()
	if f, ok := c.lru.Get(id); ok {
		c.mu.Unlock()
		return clone(f), nil
	}
	gen := c.gen
	c.mu.Unlock()

	f, err := c.FolderRepository.FindFolderByID(ctx, id)
	if err != nil {
		return model.Folder{}, err
	}
	c.store(gen, clone(f))
	return f, nil
}

// store caches f, read when the write count was gen.
func (c *cachedFolders) store(gen uint64, f model.Folder) {
	n := folderBytes(f)
	c.mu.Lock()
	defer c.mu.Unlock()
	if gen != c.gen || n > c.maxBytes {
		return
	}
	// Add doesn't call evicted when it replaces a key.
	c.lru.Remove(f.ID)
	c.lru.Add(f.ID, f)
	c.size += n
	for c.size > c.maxBytes {
		c.lru.RemoveOldest()
	}
}

// drop forgets a folder that is being written.
func (c *cachedFolders) drop(id int64) {
	c.mu.Lock()
	c.gen++
	c.lru.Remove(id)
	c.mu.Unlock()
}

// The writes below drop the folder both before and after writing: a lookup
// that starts mid-write reads the database, and must not keep what it read.

func (c *cachedFolders) UpdateFolderTitle(ctx context.Context, id int64, title string) error {
	c.drop(id)
	defer c.drop(id)
	return c.FolderRepository.UpdateFolderTitle(ctx, id, title)
}

func (c *cachedFolders) ExtendFolder(ctx context.Context, id int64, now, expiresAt time.Time) error {
	c.drop(id)
	defer c.drop(id)
	return c.FolderRepository.ExtendFolder(ctx, id, now, expiresAt)
}

func (c *cachedFolders) DeleteFolder(ctx context.Context, id int64) error {
	c.drop(id)
	defer c.drop(id)
	return c.FolderRepository.DeleteFolder(ctx, id)
}

func (c *cachedFolders) SaveFiles(ctx context.Context, folderID int64, agent string, files model.FolderFiles) error {
	c.drop(folderID)
	defer c.drop(folderID)
	return c.FolderRepository.SaveFiles(ctx, folderID, agent, files)
}

func (c *cachedFolders) DeleteFile(ctx context.Context, folderID int64, path string) error {
	c.drop(folderID)
	defer c.drop(folderID)
	return c.FolderRepository.DeleteFile(ctx, folderID, path)
}
