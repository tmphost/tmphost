// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompression(t *testing.T) {
	page := strings.Replace(_index, "<body>", "<body>"+strings.Repeat("<p>tmphost</p>", 100), 1)
	a, err := New(testConfig(t), fstest.MapFS{"index.html": {Data: []byte(page)}})
	require.NoError(t, err)

	plain := httptest.NewRequest(http.MethodGet, "/", nil)
	res, err := a.Handler().Test(plain)
	require.NoError(t, err)
	assert.Empty(t, res.Header.Get(fiber.HeaderContentEncoding))
	tag := res.Header.Get(fiber.HeaderETag)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(fiber.HeaderAcceptEncoding, "gzip")
	res, err = a.Handler().Test(req)
	require.NoError(t, err)
	assert.Equal(t, "gzip", res.Header.Get(fiber.HeaderContentEncoding))
	assert.Equal(t, fiber.HeaderAcceptEncoding, res.Header.Get(fiber.HeaderVary))
	assert.Equal(t, tag, res.Header.Get(fiber.HeaderETag), "a weak tag names both encodings")
	zr, err := gzip.NewReader(res.Body)
	require.NoError(t, err)
	b, err := io.ReadAll(zr)
	require.NoError(t, err)
	assert.Contains(t, string(b), "<p>tmphost</p>")

	// The compressed copy revalidates against the same tag.
	req.Header.Set(fiber.HeaderIfNoneMatch, tag)
	res, err = a.Handler().Test(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotModified, res.StatusCode)
}

func TestSkipCompression(t *testing.T) {
	f := fiber.New()
	f.Get("/*", func(c fiber.Ctx) error {
		if skipCompression(c) {
			return c.SendString("skip")
		}
		return c.SendString("compress")
	})
	for path, want := range map[string]string{
		"/":                     "compress",
		"/api/v1/folders":       "compress",
		"/assets/index-abc.js":  "compress",
		MCPPath:                 "skip",
		"/oauth2/authorize":     "skip",
		"/oauth2/mcp/authorize": "skip",
		"/auth/device":          "skip",
		"/auth/github/login":    "skip",
		"/authors":              "compress",
	} {
		res, err := f.Test(httptest.NewRequest(http.MethodGet, path, nil))
		require.NoError(t, err)
		b, _ := io.ReadAll(res.Body)
		assert.Equal(t, want, string(b), path)
	}
}
