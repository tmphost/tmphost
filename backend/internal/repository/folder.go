// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package repository

//go:generate mockgen -source=folder.go -destination=mocks/folder.go -package=mocks

import (
	"context"
	"errors"
	"maps"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/tmphost/tmphost/backend/internal/data/model"
)

type (
	// FolderRepository persists folders.
	FolderRepository interface {
		CreateFolder(ctx context.Context, f model.Folder) (model.Folder, error)
		FindFolderByID(ctx context.Context, id int64) (model.Folder, error)
		// ListFoldersByUser returns a user's folders that expire after now,
		// newest first.
		ListFoldersByUser(ctx context.Context, userID int64, now time.Time) ([]model.Folder, error)
		// ListExpiredFolders returns up to limit folders that expired at or
		// before now, oldest expiry first.
		ListExpiredFolders(ctx context.Context, now time.Time, limit int) ([]model.Folder, error)
		// UpdateFolderTitle sets a folder's title.
		UpdateFolderTitle(ctx context.Context, id int64, title string) error
		// ExtendFolder moves a folder's expiry to expiresAt, if it is still
		// live at now and expires before expiresAt; else ErrNotFound.
		ExtendFolder(ctx context.Context, id int64, now, expiresAt time.Time) error
		// DeleteFolder deletes a folder and its file records.
		DeleteFolder(ctx context.Context, id int64) error
		// SaveFiles records that agent wrote files into a folder: it
		// replaces their entries in the folder's Files and their records
		// of who wrote them, together.
		SaveFiles(ctx context.Context, folderID int64, agent string, files model.FolderFiles) error
		// ListFiles returns the file records of a folder.
		ListFiles(ctx context.Context, folderID int64) ([]model.File, error)
		// DeleteFile deletes the entry and the record of a folder's file,
		// if any.
		DeleteFile(ctx context.Context, folderID int64, path string) error
	}

	folderRepository struct {
		db *gorm.DB
	}
)

// NewFolderRepository returns a GORM-backed folder repository.
func NewFolderRepository(db *gorm.DB) FolderRepository {
	return &folderRepository{db: db}
}

func (r *folderRepository) CreateFolder(ctx context.Context, f model.Folder) (model.Folder, error) {
	if err := r.db.WithContext(ctx).Create(&f).Error; err != nil {
		return model.Folder{}, err
	}
	return f, nil
}

func (r *folderRepository) FindFolderByID(ctx context.Context, id int64) (model.Folder, error) {
	var f model.Folder
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&f).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Folder{}, ErrNotFound
	}
	if err != nil {
		return model.Folder{}, err
	}
	return f, nil
}

func (r *folderRepository) ListFoldersByUser(ctx context.Context, userID int64, now time.Time) ([]model.Folder, error) {
	var folders []model.Folder
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND expires_at > ?", userID, now).
		Order("id DESC").
		Find(&folders).Error
	if err != nil {
		return nil, err
	}
	return folders, nil
}

func (r *folderRepository) ListExpiredFolders(ctx context.Context, now time.Time, limit int) ([]model.Folder, error) {
	var folders []model.Folder
	err := r.db.WithContext(ctx).
		Where("expires_at <= ?", now).
		Order("expires_at, id").
		Limit(limit).
		Find(&folders).Error
	if err != nil {
		return nil, err
	}
	return folders, nil
}

func (r *folderRepository) UpdateFolderTitle(ctx context.Context, id int64, title string) error {
	return r.db.WithContext(ctx).Model(&model.Folder{}).Where("id = ?", id).Update("title", title).Error
}

func (r *folderRepository) ExtendFolder(ctx context.Context, id int64, now, expiresAt time.Time) error {
	res := r.db.WithContext(ctx).Model(&model.Folder{}).
		Where("id = ? AND expires_at > ? AND expires_at < ?", id, now, expiresAt).
		Update("expires_at", expiresAt)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *folderRepository) DeleteFolder(ctx context.Context, id int64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&model.File{}, "folder_id = ?", id).Error; err != nil {
			return err
		}
		return tx.Delete(&model.Folder{}, "id = ?", id).Error
	})
}

func (r *folderRepository) SaveFiles(ctx context.Context, folderID int64, agent string, files model.FolderFiles) error {
	if len(files) == 0 {
		return nil
	}
	records := make([]model.File, 0, len(files))
	for p, f := range files {
		records = append(records, model.File{FolderID: folderID, Path: p, Agent: agent, UpdatedAt: f.UpdatedAt})
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := updateFolderFiles(tx, folderID, func(all model.FolderFiles) {
			maps.Copy(all, files)
		})
		if err != nil {
			return err
		}
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "folder_id"}, {Name: "path"}},
			DoUpdates: clause.AssignmentColumns([]string{"agent", "updated_at"}),
		}).Create(&records).Error
	})
}

func (r *folderRepository) ListFiles(ctx context.Context, folderID int64) ([]model.File, error) {
	var files []model.File
	if err := r.db.WithContext(ctx).Where("folder_id = ?", folderID).Find(&files).Error; err != nil {
		return nil, err
	}
	return files, nil
}

func (r *folderRepository) DeleteFile(ctx context.Context, folderID int64, path string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := updateFolderFiles(tx, folderID, func(all model.FolderFiles) {
			delete(all, path)
		})
		if err != nil {
			return err
		}
		return tx.Delete(&model.File{}, "folder_id = ? AND path = ?", folderID, path).Error
	})
}

// updateFolderFiles changes a folder's Files in tx. It locks the row first
// with a no-op update, portable to SQLite and Postgres, so concurrent
// writes to one folder take turns. A folder without Files keeps none, and a
// missing folder is no error: there is nothing left to describe.
func updateFolderFiles(tx *gorm.DB, folderID int64, change func(model.FolderFiles)) error {
	if err := tx.Model(&model.Folder{}).Where("id = ?", folderID).Update("files", gorm.Expr("files")).Error; err != nil {
		return err
	}
	var folders []model.Folder
	if err := tx.Select("files").Where("id = ?", folderID).Limit(1).Find(&folders).Error; err != nil {
		return err
	}
	if len(folders) == 0 || folders[0].Files == nil {
		return nil
	}
	change(folders[0].Files)
	return tx.Model(&model.Folder{}).Where("id = ?", folderID).Update("files", folders[0].Files).Error
}
