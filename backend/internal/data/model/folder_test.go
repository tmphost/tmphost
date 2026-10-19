// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFolderFiles_ValueAndScan(t *testing.T) {
	at := time.Date(2026, 10, 19, 8, 0, 0, 0, time.UTC)
	files := FolderFiles{"a.bin": {Size: 2 << 20, ContentType: "application/octet-stream", UpdatedAt: at}}
	v, err := files.Value()
	require.NoError(t, err)
	assert.JSONEq(t, `{"a.bin":{"size":2097152,"contentType":"application/octet-stream","updatedAt":"2026-10-19T08:00:00Z"}}`, v.(string))

	var got FolderFiles
	require.NoError(t, got.Scan(v))
	assert.Equal(t, files, got)
	got = nil
	require.NoError(t, got.Scan([]byte(v.(string))), "drivers may hand back bytes")
	assert.Equal(t, files, got)

	v, err = FolderFiles(nil).Value()
	require.NoError(t, err)
	assert.Nil(t, v, "no list is NULL")
	require.NoError(t, got.Scan(nil))
	assert.Nil(t, got)

	v, err = FolderFiles{}.Value()
	require.NoError(t, err)
	assert.Equal(t, "{}", v)

	assert.Error(t, got.Scan(42))
	assert.Error(t, got.Scan("not json"))
}
