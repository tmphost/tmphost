// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"errors"

	"github.com/gofiber/fiber/v3"

	creditctrl "github.com/tmphost/tmphost/backend/internal/controller/credit"
	mapper "github.com/tmphost/tmphost/backend/internal/mapper/api"
)

// Admin handles the root-only admin page.
type Admin struct {
	credits creditctrl.Controller
}

// CreateCreditGrant grants credits, which expire, to one user or to every
// user.
func (h *Admin) CreateCreditGrant(c fiber.Ctx) error {
	rq := mapper.FromHTTPRequestToGrantCreditsRequestEntity(c)
	if rq == nil {
		return fiber.ErrBadRequest
	}
	rs, err := h.credits.GrantCredits(c.Context(), rq)
	if errors.Is(err, creditctrl.ErrInvalidGrant) {
		return sendError(c, fiber.StatusBadRequest, err.Error())
	}
	if errors.Is(err, creditctrl.ErrUserNotFound) {
		return sendError(c, fiber.StatusNotFound, "user not found")
	}
	if err != nil {
		logServerError(c, err)
		return sendError(c, fiber.StatusInternalServerError, "failed to grant credits")
	}
	return sendJSON(c, fiber.StatusCreated, mapper.FromGrantCreditsResponseEntityToHTTPResponse(rs))
}
