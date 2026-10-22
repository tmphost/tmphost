// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"

	"github.com/gofiber/fiber/v3"

	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/data/view"
)

// FromHTTPRequestToGetAccountPinRequestEntity reads GET /users/me/pin.
func FromHTTPRequestToGetAccountPinRequestEntity(c fiber.Ctx) *entity.GetAccountPinRequest {
	userID := localUserID(c)
	if userID == 0 {
		return nil
	}
	return &entity.GetAccountPinRequest{UserID: userID}
}

// FromHTTPRequestToNewTOTPRequestEntity reads POST /users/me/pin/totp.
func FromHTTPRequestToNewTOTPRequestEntity(c fiber.Ctx) *entity.NewTOTPRequest {
	userID := localUserID(c)
	if userID == 0 {
		return nil
	}
	return &entity.NewTOTPRequest{UserID: userID}
}

// FromHTTPRequestToUpdateAccountPinRequestEntity reads PUT /users/me/pin.
func FromHTTPRequestToUpdateAccountPinRequestEntity(c fiber.Ctx) *entity.UpdateAccountPinRequest {
	userID := localUserID(c)
	var body view.UpdateAccountPinRequest
	if userID == 0 || json.Unmarshal(c.Body(), &body) != nil {
		return nil
	}
	return &entity.UpdateAccountPinRequest{
		UserID:    userID,
		Mode:      body.Mode,
		Pin:       body.Pin,
		Secret:    body.Secret,
		Code:      body.Code,
		AgentPins: body.AgentPins,
	}
}

// FromAccountPinResponseEntityToHTTPResponse renders GET and PUT
// /users/me/pin.
func FromAccountPinResponseEntityToHTTPResponse(rs *entity.AccountPinResponse) []byte {
	return marshal(view.AccountPin{Mode: rs.Pin.Mode, AgentPins: rs.Pin.AgentPins})
}

// FromNewTOTPResponseEntityToHTTPResponse renders POST /users/me/pin/totp.
func FromNewTOTPResponseEntityToHTTPResponse(rs *entity.NewTOTPResponse) []byte {
	return marshal(view.TOTP{Secret: rs.Secret, URL: rs.URL, QRCode: rs.QRCode})
}
