// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"
)

// Folder is a group of uploaded files that expire together. The files
// themselves live in storage under u<UserID>/<ID>/; Files describes them,
// and who wrote each one is kept in File.
type Folder struct {
	ID        int64 `gorm:"primaryKey;autoIncrement:false"`
	CreatedAt time.Time
	UserID    int64     `gorm:"index;not null"`
	ExpiresAt time.Time `gorm:"index;not null"`
	// Title is the name the owner gave the folder; empty until renamed.
	Title string `gorm:"size:400;not null;default:''"`
	// Agent names what created the folder, like File.Agent.
	Agent string `gorm:"size:100;not null;default:''"`
	// Files lists the folder's files, so they can be listed and served
	// without asking the storage. It is nil (NULL) for a folder created
	// before the list was kept; those are listed from storage.
	Files FolderFiles `gorm:"type:text"`
	// PinHash is the bcrypt hash of the folder's own PIN; empty for none.
	// The owner's account PIN, when set, guards the folder instead.
	PinHash string `gorm:"type:varchar(100);not null;default:''"`
}

// FolderFiles maps the paths of a folder's files to what they hold. It is
// stored as a JSON object.
type FolderFiles map[string]FolderFile

// FolderFile describes one file of a folder.
type FolderFile struct {
	Size        int64     `json:"size"`
	ContentType string    `json:"contentType"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Value stores the files as JSON, and nil as NULL.
func (f FolderFiles) Value() (driver.Value, error) {
	if f == nil {
		return nil, nil
	}
	b, err := json.Marshal(f)
	return string(b), err
}

// Scan reads the files from JSON, and NULL as nil.
func (f *FolderFiles) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*f = nil
		return nil
	case string:
		return json.Unmarshal([]byte(v), f)
	case []byte:
		return json.Unmarshal(v, f)
	default:
		return errors.New("folder files: not JSON text")
	}
}
