// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package api is the HTTP (Fiber) transport for the REST API.
package api

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"

	authctrl "github.com/tmphost/tmphost/backend/internal/controller/auth"
	setupctrl "github.com/tmphost/tmphost/backend/internal/controller/setup"
	mapper "github.com/tmphost/tmphost/backend/internal/mapper/api"
)

// RoutePrefix is where the API is mounted.
const RoutePrefix = "/api/v1"

// Root sign-in attempts allowed per client IP per window, to slow down
// guessing the root access token.
const (
	_rootLoginMax    = 10
	_rootLoginWindow = time.Minute
)

// Params are the API's dependencies.
type Params struct {
	Router fiber.Router
	Auth   authctrl.Controller
	Setup  setupctrl.Controller
	// BasePath is the path prefix the app is served under ("" or "/x").
	BasePath string
	// CookieSecure marks cookies Secure; enable it behind HTTPS.
	CookieSecure bool
}

// New registers every API route on p.Router.
func New(p Params) {
	basePath := strings.TrimRight(p.BasePath, "/")
	auth := &Auth{
		controller:   p.Auth,
		basePath:     basePath,
		cookieSecure: p.CookieSecure,
	}
	user := &User{controller: p.Auth}
	setup := &Setup{controller: p.Setup, basePath: basePath, cookieSecure: p.CookieSecure}

	r := p.Router.Group(RoutePrefix)
	r.Get("/auth/config", auth.Config)
	r.Get("/auth/:provider/login", auth.Login)
	r.Get("/auth/:provider/callback", auth.Callback)
	r.Post("/auth/logout", auth.Logout)

	r.Get("/users/me", Authenticate(p.Auth), user.Get)

	r.Post("/setup/login", limiter.New(limiter.Config{
		Max:          _rootLoginMax,
		Expiration:   _rootLoginWindow,
		LimitReached: func(c *fiber.Ctx) error { return sendError(c, fiber.StatusTooManyRequests, "too many attempts") },
	}), setup.Login)
	r.Post("/setup/logout", setup.Logout)
	r.Get("/setup/settings", AuthenticateRoot(p.Setup), setup.Get)
	r.Put("/setup/settings", AuthenticateRoot(p.Setup), setup.Update)
}

func sendJSON(c *fiber.Ctx, status int, payload []byte) error {
	c.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	return c.Status(status).Send(payload)
}

func sendError(c *fiber.Ctx, status int, msg string) error {
	return sendJSON(c, status, mapper.FromErrorToHTTPResponse(msg))
}
