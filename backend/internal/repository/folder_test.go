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

func TestFolderRepository(t *testing.T) {
	ctx := context.Background()
	repo := NewFolderRepository(newDB(t))
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

	for _, f := range []model.Folder{
		{ID: 1, UserID: 7, ExpiresAt: now.Add(-time.Minute)},
		{ID: 2, UserID: 7, ExpiresAt: now.Add(time.Hour)},
		{ID: 3, UserID: 7, ExpiresAt: now.Add(2 * time.Hour)},
		{ID: 4, UserID: 8, ExpiresAt: now.Add(-2 * time.Minute)},
		{ID: 5, UserID: 8, ExpiresAt: now},
	} {
		_, err := repo.CreateFolder(ctx, f)
		require.NoError(t, err)
	}
	_, err := repo.CreateFolder(ctx, model.Folder{ID: 1, UserID: 9})
	assert.Error(t, err, "ids are unique")

	got, err := repo.FindFolderByID(ctx, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(7), got.UserID)
	assert.True(t, now.Add(time.Hour).Equal(got.ExpiresAt))
	_, err = repo.FindFolderByID(ctx, 99)
	assert.ErrorIs(t, err, ErrNotFound)

	live, err := repo.ListFoldersByUser(ctx, 7, now)
	require.NoError(t, err)
	assert.Equal(t, []int64{3, 2}, folderIDs(live), "live only, newest first")

	expired, err := repo.ListExpiredFolders(ctx, now, 10)
	require.NoError(t, err)
	assert.Equal(t, []int64{4, 1, 5}, folderIDs(expired), "oldest expiry first")
	expired, err = repo.ListExpiredFolders(ctx, now, 1)
	require.NoError(t, err)
	assert.Len(t, expired, 1)

	require.NoError(t, repo.UpdateFolderTitle(ctx, 2, "Holiday photos"))
	got, err = repo.FindFolderByID(ctx, 2)
	require.NoError(t, err)
	assert.Equal(t, "Holiday photos", got.Title)
	got, err = repo.FindFolderByID(ctx, 3)
	require.NoError(t, err)
	assert.Empty(t, got.Title, "untitled by default")

	require.NoError(t, repo.ExtendFolder(ctx, 2, now, now.Add(3*time.Hour)))
	got, err = repo.FindFolderByID(ctx, 2)
	require.NoError(t, err)
	assert.True(t, now.Add(3*time.Hour).Equal(got.ExpiresAt))
	assert.ErrorIs(t, repo.ExtendFolder(ctx, 1, now, now.Add(time.Hour)), ErrNotFound, "expired")
	assert.ErrorIs(t, repo.ExtendFolder(ctx, 3, now, now.Add(time.Hour)), ErrNotFound, "never shortens")
	assert.ErrorIs(t, repo.ExtendFolder(ctx, 99, now, now.Add(time.Hour)), ErrNotFound)

	require.NoError(t, repo.DeleteFolder(ctx, 4))
	_, err = repo.FindFolderByID(ctx, 4)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestFolderRepository_Files(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	repo := NewFolderRepository(db)
	now := time.Date(2026, 10, 19, 7, 0, 0, 0, time.UTC)
	_, err := repo.CreateFolder(ctx, model.Folder{ID: 1, UserID: 7, ExpiresAt: now.Add(time.Hour), Agent: "Claude Code"})
	require.NoError(t, err)
	got, err := repo.FindFolderByID(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, "Claude Code", got.Agent)

	require.NoError(t, repo.SaveFiles(ctx, nil), "nothing to save")
	require.NoError(t, repo.SaveFiles(ctx, []model.File{
		{FolderID: 1, Path: "a.txt", Agent: "Claude Code", UpdatedAt: now},
		{FolderID: 1, Path: "css/b.css", Agent: "Claude Code", UpdatedAt: now},
		{FolderID: 2, Path: "a.txt", Agent: "Web", UpdatedAt: now},
	}))
	// Writing a path again replaces its record.
	require.NoError(t, repo.SaveFiles(ctx, []model.File{{FolderID: 1, Path: "a.txt", Agent: "tmphost CLI", UpdatedAt: now.Add(time.Minute)}}))
	files, err := repo.ListFiles(ctx, 1)
	require.NoError(t, err)
	agents := map[string]string{}
	for _, f := range files {
		agents[f.Path] = f.Agent
	}
	assert.Equal(t, map[string]string{"a.txt": "tmphost CLI", "css/b.css": "Claude Code"}, agents)

	require.NoError(t, repo.DeleteFile(ctx, 1, "css/b.css"))
	files, err = repo.ListFiles(ctx, 1)
	require.NoError(t, err)
	assert.Len(t, files, 1)

	require.NoError(t, repo.DeleteFolder(ctx, 1))
	files, err = repo.ListFiles(ctx, 1)
	require.NoError(t, err)
	assert.Empty(t, files, "a folder takes its file records along")
	files, err = repo.ListFiles(ctx, 2)
	require.NoError(t, err)
	assert.Len(t, files, 1, "other folders keep theirs")

	// A failing file delete keeps the folder.
	_, err = repo.CreateFolder(ctx, model.Folder{ID: 3, UserID: 7, ExpiresAt: now})
	require.NoError(t, err)
	require.NoError(t, db.Migrator().DropTable(&model.File{}))
	assert.Error(t, repo.DeleteFolder(ctx, 3))
	_, err = repo.FindFolderByID(ctx, 3)
	assert.NoError(t, err)
}

func TestFolderRepository_DBErrors(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	repo := NewFolderRepository(db)

	_, err = repo.FindFolderByID(ctx, 1)
	assert.Error(t, err)
	assert.NotErrorIs(t, err, ErrNotFound)
	_, err = repo.ListFoldersByUser(ctx, 1, time.Now())
	assert.Error(t, err)
	_, err = repo.ListExpiredFolders(ctx, time.Now(), 1)
	assert.Error(t, err)
	assert.Error(t, repo.UpdateFolderTitle(ctx, 1, "x"))
	assert.Error(t, repo.ExtendFolder(ctx, 1, time.Now(), time.Now()))
	assert.Error(t, repo.DeleteFolder(ctx, 1))
	assert.Error(t, repo.SaveFiles(ctx, []model.File{{FolderID: 1, Path: "a"}}))
	_, err = repo.ListFiles(ctx, 1)
	assert.Error(t, err)
	assert.Error(t, repo.DeleteFile(ctx, 1, "a"))
}

func folderIDs(fs []model.Folder) []int64 {
	ids := []int64{}
	for _, f := range fs {
		ids = append(ids, f.ID)
	}
	return ids
}
