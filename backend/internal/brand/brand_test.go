// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package brand

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFavicon(t *testing.T) {
	require.Greater(t, len(Favicon), 6)
	// An ICO header: reserved 0, type 1 (icon), then the image count.
	assert.Equal(t, uint16(0), binary.LittleEndian.Uint16(Favicon[0:]))
	assert.Equal(t, uint16(1), binary.LittleEndian.Uint16(Favicon[2:]))
	assert.Equal(t, uint16(3), binary.LittleEndian.Uint16(Favicon[4:]))
}

func TestIcons(t *testing.T) {
	assert.True(t, bytes.HasPrefix(IconPNG, []byte("\x89PNG\r\n\x1a\n")))
	assert.True(t, bytes.HasPrefix(IconSVG, []byte("<svg")))
}

func TestDataURI(t *testing.T) {
	assert.Equal(t, "data:image/png;base64,aGk=", DataURI(IconPNGType, []byte("hi")))
}
