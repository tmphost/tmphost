// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"github.com/gofiber/fiber/v3"

	creditctrl "github.com/tmphost/tmphost/backend/internal/controller/credit"
	mapper "github.com/tmphost/tmphost/backend/internal/mapper/api"
)

// Credit handles the signed-in user's credit history.
type Credit struct {
	controller creditctrl.Controller
}

// List returns a page of the user's credit activities, newest first.
func (h *Credit) List(c fiber.Ctx) error {
	rq := mapper.FromHTTPRequestToListCreditActivitiesRequestEntity(c)
	if rq == nil {
		return fiber.ErrBadRequest
	}
	rs, err := h.controller.ListCreditActivities(c.Context(), rq)
	if err != nil {
		return sendError(c, fiber.StatusInternalServerError, "failed to load credit history")
	}
	return sendJSON(c, fiber.StatusOK, mapper.FromListCreditActivitiesResponseEntityToHTTPResponse(rs))
}
