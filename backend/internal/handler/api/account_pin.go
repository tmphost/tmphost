// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"errors"

	"github.com/gofiber/fiber/v3"

	"github.com/tmphost/tmphost/backend/internal/controller/accountpin"
	mapper "github.com/tmphost/tmphost/backend/internal/mapper/api"
)

// AccountPin handles the signed-in user's account PIN, which guards every
// folder of theirs.
type AccountPin struct {
	controller accountpin.Controller
}

// Get returns the account PIN settings.
func (h *AccountPin) Get(c fiber.Ctx) error {
	rq := mapper.FromHTTPRequestToGetAccountPinRequestEntity(c)
	if rq == nil {
		return fiber.ErrBadRequest
	}
	rs, err := h.controller.GetAccountPin(c.Context(), rq)
	if err != nil {
		return sendAccountPinError(c, err)
	}
	return sendJSON(c, fiber.StatusOK, mapper.FromAccountPinResponseEntityToHTTPResponse(rs))
}

// NewTOTP returns a new authenticator secret to scan; it is saved by
// Update, with a code.
func (h *AccountPin) NewTOTP(c fiber.Ctx) error {
	rq := mapper.FromHTTPRequestToNewTOTPRequestEntity(c)
	if rq == nil {
		return fiber.ErrBadRequest
	}
	rs, err := h.controller.NewTOTP(c.Context(), rq)
	if err != nil {
		return sendAccountPinError(c, err)
	}
	c.Set(fiber.HeaderCacheControl, "no-store")
	return sendJSON(c, fiber.StatusOK, mapper.FromNewTOTPResponseEntityToHTTPResponse(rs))
}

// Update changes the account PIN settings.
func (h *AccountPin) Update(c fiber.Ctx) error {
	rq := mapper.FromHTTPRequestToUpdateAccountPinRequestEntity(c)
	if rq == nil {
		return fiber.ErrBadRequest
	}
	rs, err := h.controller.UpdateAccountPin(c.Context(), rq)
	if err != nil {
		return sendAccountPinError(c, err)
	}
	return sendJSON(c, fiber.StatusOK, mapper.FromAccountPinResponseEntityToHTTPResponse(rs))
}

func sendAccountPinError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, accountpin.ErrNotFound):
		return sendError(c, fiber.StatusNotFound, err.Error())
	case errors.Is(err, accountpin.ErrInvalidMode), errors.Is(err, accountpin.ErrInvalidPin), errors.Is(err, accountpin.ErrInvalidCode):
		return sendError(c, fiber.StatusBadRequest, err.Error())
	default:
		logServerError(c, err)
		return sendError(c, fiber.StatusInternalServerError, "failed to update the pin")
	}
}
