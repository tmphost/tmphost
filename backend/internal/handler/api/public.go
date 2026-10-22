// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	folderctrl "github.com/tmphost/tmphost/backend/internal/controller/folder"
	"github.com/tmphost/tmphost/backend/internal/data/entity"
	mapper "github.com/tmphost/tmphost/backend/internal/mapper/api"
)

// _sandbox keeps uploaded pages working (scripts, forms, popups) but in an
// opaque origin, so they can't read or set cookies of any tmphost origin,
// or call the API as the visitor, even when served on the app's origin.
const _sandbox = "sandbox allow-scripts allow-forms allow-popups allow-modals allow-downloads"

// Public serves folders to anyone: on folder hosts,
// <folderID>-<userID>.<domain>, with subdomains on, else below
// /files/<userID>/<folderID>/.
type Public struct {
	controller folderctrl.Controller
	// settings returns the current file settings; the mode can change at
	// runtime.
	settings func() entity.FileSettings
	// cookieSecure marks the PIN cookie Secure; enable it behind HTTPS.
	cookieSecure bool
}

// Serve answers every request to a folder host and passes any other
// request on. It runs before every route, so no app route is ever
// reachable on a host that serves user content.
func (h *Public) Serve(c fiber.Ctx) error {
	s := h.settings()
	if !s.Subdomains {
		return c.Next()
	}
	rq := mapper.FromHTTPRequestToOpenPublicFileRequestEntity(c, s.PublicDomain)
	if rq == nil {
		return c.Next()
	}
	if "/"+rq.Path == FaviconPath {
		// A folder's own favicon.ico wins; any other gets ours.
		return h.send(c, rq, Favicon)
	}
	return h.send(c, rq, notFound)
}

// ServePath answers /files/<userID>/<folderID>/<path> when subdomains are
// off; otherwise the path is left to the web app.
func (h *Public) ServePath(c fiber.Ctx) error {
	if h.settings().Subdomains {
		return c.Next()
	}
	rq := mapper.FromHTTPRequestToOpenPublicFileByPathRequestEntity(c)
	if rq == nil {
		return notFound(c)
	}
	// Relative links in the folder's pages need the trailing slash.
	if rq.Path == "" && !strings.HasSuffix(c.Path(), "/") {
		return c.Redirect().Status(fiber.StatusMovedPermanently).To(c.Params("folder") + "/")
	}
	return h.send(c, rq, notFound)
}

// send answers with the requested file, or with missing when there is no
// such file. A PIN-guarded folder asks for its PIN first, on a page that
// posts it back to the same URL.
func (h *Public) send(c fiber.Ctx, rq *entity.OpenPublicFileRequest, missing fiber.Handler) error {
	if c.Method() == fiber.MethodPost {
		return h.unlock(c, rq, missing)
	}
	if c.Method() != fiber.MethodGet && c.Method() != fiber.MethodHead {
		return c.SendStatus(fiber.StatusMethodNotAllowed)
	}
	rs, err := h.controller.OpenPublicFile(c.Context(), rq)
	if err != nil {
		return h.sendError(c, err, missing)
	}
	if rs.Unlock != nil {
		h.setPinCookie(c, rq, rs.Unlock)
	}
	// Files of a guarded folder must not be kept by shared caches, which
	// would hand them to visitors without the PIN.
	cache := "public, max-age=60"
	if rs.Private {
		cache = "private, max-age=60"
	}
	if rs.RedirectURL != "" {
		// Large files come straight from the bucket; the signed URL
		// outlives this cache time.
		c.Set(fiber.HeaderCacheControl, cache)
		return c.Redirect().Status(fiber.StatusFound).To(rs.RedirectURL)
	}
	c.Set(fiber.HeaderContentType, rs.ContentType)
	c.Set(fiber.HeaderContentSecurityPolicy, _sandbox)
	c.Set(fiber.HeaderXContentTypeOptions, "nosniff")
	c.Set(fiber.HeaderCacheControl, cache)
	c.Set(fiber.HeaderLastModified, rs.ModTime.UTC().Format(http.TimeFormat))
	return c.SendStream(rs.Body, int(rs.Size))
}

// unlock checks the PIN posted from the PIN page; with the right one, it
// keeps the visitor's token in a cookie and sends them back to the URL
// they asked for.
func (h *Public) unlock(c fiber.Ctx, rq *entity.OpenPublicFileRequest, missing fiber.Handler) error {
	rs, err := h.controller.UnlockFolder(c.Context(), mapper.FromOpenPublicFileRequestEntityToUnlockFolderRequestEntity(rq))
	if err != nil {
		return h.sendError(c, err, missing)
	}
	if rs.Token != "" {
		h.setPinCookie(c, rq, rs)
	}
	return c.Redirect().Status(fiber.StatusSeeOther).To(withoutPin(c))
}

// sendError answers a visitor whose file could not be opened. Visitors get
// plain text or the PIN page, not the API's JSON errors.
func (h *Public) sendError(c fiber.Ctx, err error, missing fiber.Handler) error {
	switch {
	case errors.Is(err, folderctrl.ErrNotFound):
		return missing(c)
	case errors.Is(err, folderctrl.ErrPinRequired):
		return sendPage(c, fiber.StatusUnauthorized, pinPage(""))
	case errors.Is(err, folderctrl.ErrWrongPin):
		return sendPage(c, fiber.StatusUnauthorized, pinPage("Wrong PIN. Try again."))
	case errors.Is(err, folderctrl.ErrPinRateLimited):
		c.Set(fiber.HeaderRetryAfter, strconv.Itoa(int(folderctrl.PinWindow/time.Second)))
		return sendPage(c, fiber.StatusTooManyRequests, pinPage("Too many wrong PINs. Wait a minute, then try again."))
	default:
		logServerError(c, err)
		return c.Status(fiber.StatusInternalServerError).SendString("Something went wrong.")
	}
}

// setPinCookie keeps a visitor's token for a folder until the folder
// expires: on the folder's own host, or below its path on the app's.
// Behind HTTPS it is SameSite=None, so the sandboxed pages of the folder,
// whose origin is opaque, still send it with the files they load.
func (h *Public) setPinCookie(c fiber.Ctx, rq *entity.OpenPublicFileRequest, unlock *entity.UnlockFolderResponse) {
	path := "/"
	if rq.UserID != 0 {
		path = folderPath(c.Path())
	}
	sameSite := fiber.CookieSameSiteLaxMode
	if h.cookieSecure {
		sameSite = fiber.CookieSameSiteNoneMode
	}
	c.Cookie(&fiber.Cookie{
		Name:     mapper.PinCookie,
		Value:    unlock.Token,
		Path:     path,
		Expires:  unlock.ExpiresAt,
		HTTPOnly: true,
		Secure:   h.cookieSecure,
		SameSite: sameSite,
	})
}

// folderPath is the path of the folder a /files/<userID>/<folderID>/...
// path is in.
func folderPath(p string) string {
	i := strings.Index(p, folderctrl.PublicPathPrefix) + len(folderctrl.PublicPathPrefix)
	segs := strings.SplitN(p[i:], "/", 3)
	return p[:i] + segs[0] + "/" + segs[1] + "/"
}

// withoutPin is the URL asked for, without its ?pin=, which is not to be
// kept in the address bar or the history.
func withoutPin(c fiber.Ctx) string {
	q, _ := url.ParseQuery(string(c.Request().URI().QueryString()))
	q.Del("pin")
	if len(q) == 0 {
		return c.Path()
	}
	return c.Path() + "?" + q.Encode()
}

func notFound(c fiber.Ctx) error {
	return c.Status(fiber.StatusNotFound).SendString("Not found. The file may have expired.")
}
