// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"

	"github.com/tmphost/tmphost/backend/internal/data/entity"
)

func TestFromHTTPRequestToFolderPinRequestEntity(t *testing.T) {
	run := func(userID int64, target string) *entity.FolderPinRequest {
		var rq *entity.FolderPinRequest
		runAs(t, userID, "/folders/:id/pin", httptest.NewRequest(http.MethodPost, target, nil), func(c fiber.Ctx) {
			rq = FromHTTPRequestToFolderPinRequestEntity(c)
		})
		return rq
	}
	assert.Equal(t, &entity.FolderPinRequest{UserID: 7, FolderID: 1000}, run(7, "/folders/000000rs/pin"))
	assert.Nil(t, run(7, "/folders/-/pin"))
	assert.Nil(t, run(0, "/folders/000000rs/pin"))
}

func TestAccountPinRequests(t *testing.T) {
	var (
		get    *entity.GetAccountPinRequest
		totp   *entity.NewTOTPRequest
		update *entity.UpdateAccountPinRequest
	)
	run := func(userID int64, body string) {
		get, totp, update = nil, nil, nil
		runAs(t, userID, "/pin", httptest.NewRequest(http.MethodPut, "/pin", strings.NewReader(body)), func(c fiber.Ctx) {
			get = FromHTTPRequestToGetAccountPinRequestEntity(c)
			totp = FromHTTPRequestToNewTOTPRequestEntity(c)
			update = FromHTTPRequestToUpdateAccountPinRequestEntity(c)
		})
	}

	run(7, `{"mode":"totp","pin":"1","secret":"S","code":"123456","agentPins":true}`)
	assert.Equal(t, &entity.GetAccountPinRequest{UserID: 7}, get)
	assert.Equal(t, &entity.NewTOTPRequest{UserID: 7}, totp)
	assert.Equal(t, &entity.UpdateAccountPinRequest{UserID: 7, Mode: "totp", Pin: "1", Secret: "S", Code: "123456", AgentPins: true}, update)

	run(7, `nope`)
	assert.Nil(t, update)

	run(0, `{"mode":"off"}`)
	assert.Nil(t, get)
	assert.Nil(t, totp)
	assert.Nil(t, update)
}

func TestAccountPinResponses(t *testing.T) {
	assert.JSONEq(t, `{"mode":"static","agentPins":false}`,
		string(FromAccountPinResponseEntityToHTTPResponse(&entity.AccountPinResponse{Pin: entity.AccountPin{Mode: entity.PinModeStatic}})))
	assert.JSONEq(t, `{"secret":"S","url":"otpauth://x","qrCode":"data:"}`,
		string(FromNewTOTPResponseEntityToHTTPResponse(&entity.NewTOTPResponse{Secret: "S", URL: "otpauth://x", QRCode: "data:"})))
	assert.Contains(t, string(FromFolderPinResponseEntityToHTTPResponse(&entity.FolderPinResponse{Folder: entity.Folder{ID: 1000, PinMode: entity.PinModeFolder, Pin: "123456"}})),
		`"pinMode":"folder","pin":"123456"`)
}
