// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package model

import "time"

// File records who last wrote a file of a folder. The bytes live in
// storage; a row is keyed by its folder and path, so writing the same path
// again replaces it.
type File struct {
	FolderID int64  `gorm:"primaryKey;autoIncrement:false"`
	Path     string `gorm:"primaryKey;size:512"`
	// Agent names what uploaded the file: the OAuth client's name (e.g.
	// "Claude Code", "tmphost CLI") or "Web".
	Agent     string `gorm:"size:100;not null;default:''"`
	UpdatedAt time.Time
}
