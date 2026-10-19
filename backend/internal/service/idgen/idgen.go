// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package idgen generates time-ordered, unique int64 IDs.
package idgen

//go:generate mockgen -source=idgen.go -destination=mocks/idgen.go -package=mocks

import (
	"crypto/rand"
	"encoding/binary"
	"strconv"
	"strings"

	"github.com/mustafaturan/monoflake"
)

type (
	// Service issues new IDs.
	Service interface {
		NextID() int64
	}

	service struct {
		mf *monoflake.MonoFlake
	}
)

// New returns an ID generator for the given node. Node IDs wrap at 1024.
func New(node uint16) Service {
	// monoflake.New only fails on invalid options, and none are passed.
	mf, _ := monoflake.New(node)
	return &service{mf: mf}
}

func (s *service) NextID() int64 {
	return s.mf.Next().Int64()
}

// FormatBase36 writes an ID in lowercase base36. Unlike base62 it survives
// case-insensitive contexts such as hostnames.
func FormatBase36(id int64) string {
	return strconv.FormatInt(id, 36)
}

// ParseBase36 reads an ID written by FormatBase36. It returns 0 for
// anything else, including uppercase or non-positive values.
func ParseBase36(s string) int64 {
	if s == "" || strings.ToLower(s) != s {
		return 0
	}
	id, err := strconv.ParseInt(s, 36, 64)
	if err != nil || id <= 0 {
		return 0
	}
	return id
}

// FolderIDLength is the length of a written folder ID: 11 base36
// characters, so "f" and the ID make a dynamic subdomain's label.
const FolderIDLength = 11

// _folderIDSpace bounds folder IDs: 56 random bits fit in 11 base36
// characters.
const _folderIDSpace = 1<<56 - 1

// NewFolderID returns a random folder ID in [1, 2^56). Folder IDs are
// random rather than time-ordered so public URLs can't be guessed.
func NewFolderID() int64 {
	var b [8]byte
	// crypto/rand.Read never fails.
	_, _ = rand.Read(b[:])
	return int64(binary.BigEndian.Uint64(b[:])%_folderIDSpace) + 1
}

// FormatFolderID writes a folder ID as FolderIDLength lowercase base36
// characters, zero-padded.
func FormatFolderID(id int64) string {
	s := FormatBase36(id)
	if len(s) < FolderIDLength {
		s = strings.Repeat("0", FolderIDLength-len(s)) + s
	}
	return s
}

// ParseFolderID reads an ID written by FormatFolderID. It returns 0 for
// anything else.
func ParseFolderID(s string) int64 {
	if len(s) != FolderIDLength {
		return 0
	}
	return ParseBase36(s)
}
