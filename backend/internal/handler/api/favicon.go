// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"github.com/gofiber/fiber/v3"

	"github.com/tmphost/tmphost/backend/internal/brand"
)

// FaviconPath is where browsers look for a site's icon. Every tmphost host
// answers it: the app, the MCP host and folder subdomains.
const FaviconPath = "/favicon.ico"

// _faviconETag names the icon of this build.
var _faviconETag = ContentETag(brand.Favicon)

// Favicon sends the tmphost.io icon.
func Favicon(c fiber.Ctx) error {
	c.Set(fiber.HeaderContentType, brand.FaviconType)
	c.Set(fiber.HeaderCacheControl, "public, max-age=86400")
	return SendWithETag(c, _faviconETag, brand.Favicon)
}
