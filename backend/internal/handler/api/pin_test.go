// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tmphost/tmphost/backend/internal/controller/accountpin"
	folderctrl "github.com/tmphost/tmphost/backend/internal/controller/folder"
	mockfolder "github.com/tmphost/tmphost/backend/internal/controller/folder/mocks"
	"github.com/tmphost/tmphost/backend/internal/data/entity"
	mapper "github.com/tmphost/tmphost/backend/internal/mapper/api"
)

// visit sends a visitor's request, without the API's bearer token.
func (e *filesEnv) visit(t *testing.T, method, host, target, form string, cookies ...*http.Cookie) (*http.Response, string) {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(form))
	if host != "" {
		req.Host = host
	}
	if form != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	res, err := e.app.Test(req)
	require.NoError(t, err)
	b, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	return res, string(b)
}

func file(body string) *entity.OpenFileResponse {
	return &entity.OpenFileResponse{Body: io.NopCloser(strings.NewReader(body)), Size: int64(len(body)), ContentType: "text/html; charset=utf-8", ModTime: _at}
}

func TestPublic_PinPage(t *testing.T) {
	e := newFilesApp(t)
	const host = "000000rs.tmp.host"

	e.folders.EXPECT().OpenPublicFile(gomock.Any(), &entity.OpenPublicFileRequest{FolderID: 1000, Client: "0.0.0.0"}).Return(nil, folderctrl.ErrPinRequired)
	res, body := e.visit(t, http.MethodGet, host, "/", "")
	assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
	assert.Contains(t, body, `<form method="post" class="entry">`)
	assert.Contains(t, body, `data-test="pin-input"`)
	assert.NotContains(t, body, `data-test="pin-error"`)
	assert.Equal(t, _pageCSP, res.Header.Get("Content-Security-Policy"))
	assert.Equal(t, "no-store", res.Header.Get("Cache-Control"))

	e.folders.EXPECT().OpenPublicFile(gomock.Any(), &entity.OpenPublicFileRequest{FolderID: 1000, Pin: "111111", Client: "0.0.0.0"}).Return(nil, folderctrl.ErrWrongPin)
	res, body = e.visit(t, http.MethodGet, host, "/?pin=111111", "")
	assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
	assert.Contains(t, body, "Wrong PIN. Try again.")

	e.folders.EXPECT().OpenPublicFile(gomock.Any(), gomock.Any()).Return(nil, folderctrl.ErrPinRateLimited)
	res, body = e.visit(t, http.MethodGet, host, "/?pin=111111", "")
	assert.Equal(t, http.StatusTooManyRequests, res.StatusCode)
	assert.Equal(t, "60", res.Header.Get("Retry-After"))
	assert.Contains(t, body, "Too many wrong PINs")
}

func TestPublic_PinQuery(t *testing.T) {
	e := newFilesApp(t)
	expires := _at.Add(time.Hour)
	e.folders.EXPECT().OpenPublicFile(gomock.Any(), &entity.OpenPublicFileRequest{FolderID: 1000, Path: "a.html", Pin: "123456", Client: "0.0.0.0"}).
		DoAndReturn(func(_ any, _ *entity.OpenPublicFileRequest) (*entity.OpenFileResponse, error) {
			rs := file("<p>")
			rs.Private, rs.Unlock = true, &entity.UnlockFolderResponse{Token: "tok", ExpiresAt: expires}
			return rs, nil
		})
	res, body := e.visit(t, http.MethodGet, "000000rs.tmp.host", "/a.html?pin=123456", "")
	assert.Equal(t, http.StatusOK, res.StatusCode, "a right PIN opens the file at once")
	assert.Equal(t, "<p>", body)
	assert.Equal(t, "private, max-age=60", res.Header.Get("Cache-Control"))
	ck := cookieNamed(res, mapper.PinCookie)
	require.NotNil(t, ck)
	assert.Equal(t, "tok", ck.Value)
	assert.Equal(t, "/", ck.Path)
	assert.True(t, ck.HttpOnly)
	assert.False(t, ck.Secure)
	assert.Equal(t, http.SameSiteLaxMode, ck.SameSite)
	assert.Equal(t, expires.Unix(), ck.Expires.Unix())

	// The cookie travels on; a large file of a guarded folder stays private.
	e.folders.EXPECT().OpenPublicFile(gomock.Any(), &entity.OpenPublicFileRequest{FolderID: 1000, Path: "big.png", PinToken: "tok", Client: "0.0.0.0"}).
		Return(&entity.OpenFileResponse{RedirectURL: "https://bucket.example/big.png", Private: true}, nil)
	res, _ = e.visit(t, http.MethodGet, "000000rs.tmp.host", "/big.png", "", &http.Cookie{Name: mapper.PinCookie, Value: "tok"})
	assert.Equal(t, http.StatusFound, res.StatusCode)
	assert.Equal(t, "private, max-age=60", res.Header.Get("Cache-Control"))
	assert.Nil(t, cookieNamed(res, mapper.PinCookie), "nothing new to keep")
}

func TestPublic_PinForm(t *testing.T) {
	e := newFilesApp(t)
	const host = "000000rs.tmp.host"
	expires := _at.Add(time.Hour)

	e.folders.EXPECT().UnlockFolder(gomock.Any(), &entity.UnlockFolderRequest{FolderID: 1000, Pin: "123456", Client: "0.0.0.0"}).
		Return(&entity.UnlockFolderResponse{Token: "tok", ExpiresAt: expires}, nil)
	res, _ := e.visit(t, http.MethodPost, host, "/docs/?format=html&pin=000000", url.Values{"pin": {"123456"}}.Encode())
	assert.Equal(t, http.StatusSeeOther, res.StatusCode)
	assert.Equal(t, "/docs/?format=html", res.Header.Get("Location"), "back where they were, without ?pin=")
	ck := cookieNamed(res, mapper.PinCookie)
	require.NotNil(t, ck)
	assert.Equal(t, "tok", ck.Value)

	// An open folder needs no cookie.
	e.folders.EXPECT().UnlockFolder(gomock.Any(), gomock.Any()).Return(&entity.UnlockFolderResponse{}, nil)
	res, _ = e.visit(t, http.MethodPost, host, "/a.html?pin=1", url.Values{"pin": {"123456"}}.Encode())
	assert.Equal(t, http.StatusSeeOther, res.StatusCode)
	assert.Equal(t, "/a.html", res.Header.Get("Location"))
	assert.Nil(t, cookieNamed(res, mapper.PinCookie))

	e.folders.EXPECT().UnlockFolder(gomock.Any(), gomock.Any()).Return(nil, folderctrl.ErrWrongPin)
	res, body := e.visit(t, http.MethodPost, host, "/", url.Values{"pin": {"000000"}}.Encode())
	assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
	assert.Contains(t, body, "Wrong PIN")

	e.folders.EXPECT().UnlockFolder(gomock.Any(), gomock.Any()).Return(nil, folderctrl.ErrNotFound)
	res, _ = e.visit(t, http.MethodPost, host, "/", url.Values{"pin": {"000000"}}.Encode())
	assert.Equal(t, http.StatusNotFound, res.StatusCode)

	logs := captureLog(t)
	e.folders.EXPECT().UnlockFolder(gomock.Any(), gomock.Any()).Return(nil, errBoom)
	res, _ = e.visit(t, http.MethodPost, host, "/", url.Values{"pin": {"000000"}}.Encode())
	assert.Equal(t, http.StatusInternalServerError, res.StatusCode)
	assert.Contains(t, logs.String(), "boom")
}

func TestPublic_PinPathMode(t *testing.T) {
	e := newFilesApp(t)
	e.settings.Subdomains = false
	e.folders.EXPECT().UnlockFolder(gomock.Any(), &entity.UnlockFolderRequest{UserID: 7, FolderID: 1000, Pin: "123456", Client: "0.0.0.0"}).
		Return(&entity.UnlockFolderResponse{Token: "tok", ExpiresAt: _at.Add(time.Hour)}, nil)
	res, _ := e.visit(t, http.MethodPost, "", "/files/7/000000rs/site/a.html", url.Values{"pin": {"123456"}}.Encode())
	assert.Equal(t, http.StatusSeeOther, res.StatusCode)
	assert.Equal(t, "/files/7/000000rs/site/a.html", res.Header.Get("Location"))
	ck := cookieNamed(res, mapper.PinCookie)
	require.NotNil(t, ck)
	assert.Equal(t, "/files/7/000000rs/", ck.Path, "only this folder gets it")
}

func TestPublic_PinCookieSecure(t *testing.T) {
	mc := gomock.NewController(t)
	folders := mockfolder.NewMockController(mc)
	app := fiber.New()
	New(Params{Router: app, Folders: folders, CookieSecure: true,
		FileSettings: func() entity.FileSettings { return entity.FileSettings{Subdomains: true, PublicDomain: "tmp.host"} }})
	folders.EXPECT().UnlockFolder(gomock.Any(), gomock.Any()).Return(&entity.UnlockFolderResponse{Token: "tok", ExpiresAt: _at.Add(time.Hour)}, nil)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("pin=123456"))
	req.Host = "000000rs.tmp.host"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := app.Test(req)
	require.NoError(t, err)
	ck := cookieNamed(res, mapper.PinCookie)
	require.NotNil(t, ck)
	assert.True(t, ck.Secure)
	assert.Equal(t, http.SameSiteNoneMode, ck.SameSite, "sandboxed pages load the folder's files cross-site")
}

func TestFolder_Pin(t *testing.T) {
	e := newFilesApp(t)
	folder := entity.Folder{ID: 1000, CreatedAt: _at, ExpiresAt: _at.Add(time.Hour), URL: "https://000000rs.tmp.host/", DefaultTitle: "000000rs", PinMode: entity.PinModeFolder, Pin: "123456"}

	e.folders.EXPECT().CreateFolderPin(gomock.Any(), &entity.FolderPinRequest{UserID: 7, FolderID: 1000}).Return(&entity.FolderPinResponse{Folder: folder}, nil)
	res, body := e.do(t, http.MethodPost, "/api/v1/folders/000000rs/pin", "")
	assert.Equal(t, http.StatusCreated, res.StatusCode)
	assert.Equal(t, "no-store", res.Header.Get("Cache-Control"))
	assert.Contains(t, body, `"pinMode":"folder","pin":"123456"`)

	e.folders.EXPECT().CreateFolderPin(gomock.Any(), gomock.Any()).Return(nil, folderctrl.ErrAccountPin)
	res, body = e.do(t, http.MethodPost, "/api/v1/folders/000000rs/pin", "")
	assert.Equal(t, http.StatusConflict, res.StatusCode)
	assert.JSONEq(t, `{"error":"the account pin guards this folder"}`, body)

	folder.PinMode, folder.Pin = "", ""
	e.folders.EXPECT().DeleteFolderPin(gomock.Any(), &entity.FolderPinRequest{UserID: 7, FolderID: 1000}).Return(&entity.FolderPinResponse{Folder: folder}, nil)
	res, body = e.do(t, http.MethodDelete, "/api/v1/folders/000000rs/pin", "")
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.NotContains(t, body, "pin")

	e.folders.EXPECT().DeleteFolderPin(gomock.Any(), gomock.Any()).Return(nil, folderctrl.ErrNotFound)
	res, _ = e.do(t, http.MethodDelete, "/api/v1/folders/000000rs/pin", "")
	assert.Equal(t, http.StatusNotFound, res.StatusCode)

	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		res, _ = e.do(t, method, "/api/v1/folders/bad!/pin", "")
		assert.Equal(t, http.StatusBadRequest, res.StatusCode)
	}

	// Asking for a PIN when creating a folder.
	e.folders.EXPECT().CreateFolder(gomock.Any(), &entity.CreateFolderRequest{UserID: 7, Agent: "tmphost CLI", Pin: true}).
		Return(&entity.CreateFolderResponse{Folder: entity.Folder{ID: 1000, PinMode: entity.PinModeFolder, Pin: "654321"}}, nil)
	res, body = e.do(t, http.MethodPost, "/api/v1/folders", `{"pin":true}`)
	assert.Equal(t, http.StatusCreated, res.StatusCode)
	assert.Contains(t, body, `"pin":"654321"`)
}

func TestAccountPin(t *testing.T) {
	e := newFilesApp(t)

	e.pins.EXPECT().GetAccountPin(gomock.Any(), &entity.GetAccountPinRequest{UserID: 7}).
		Return(&entity.AccountPinResponse{Pin: entity.AccountPin{Mode: entity.PinModeOff, AgentPins: true}}, nil)
	res, body := e.do(t, http.MethodGet, "/api/v1/users/me/pin", "")
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.JSONEq(t, `{"mode":"off","agentPins":true}`, body)

	e.pins.EXPECT().NewTOTP(gomock.Any(), &entity.NewTOTPRequest{UserID: 7}).
		Return(&entity.NewTOTPResponse{Secret: "S", URL: "otpauth://totp/x", QRCode: "data:image/png;base64,AA"}, nil)
	res, body = e.do(t, http.MethodPost, "/api/v1/users/me/pin/totp", "")
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, "no-store", res.Header.Get("Cache-Control"))
	assert.JSONEq(t, `{"secret":"S","url":"otpauth://totp/x","qrCode":"data:image/png;base64,AA"}`, body)

	e.pins.EXPECT().UpdateAccountPin(gomock.Any(), &entity.UpdateAccountPinRequest{UserID: 7, Mode: "totp", Secret: "S", Code: "123456", AgentPins: true}).
		Return(&entity.AccountPinResponse{Pin: entity.AccountPin{Mode: entity.PinModeTOTP, AgentPins: true}}, nil)
	res, body = e.do(t, http.MethodPut, "/api/v1/users/me/pin", `{"mode":"totp","secret":"S","code":"123456","agentPins":true}`)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.JSONEq(t, `{"mode":"totp","agentPins":true}`, body)

	res, _ = e.do(t, http.MethodPut, "/api/v1/users/me/pin", `nope`)
	assert.Equal(t, http.StatusBadRequest, res.StatusCode)

	for err, status := range map[error]int{
		accountpin.ErrNotFound:    http.StatusNotFound,
		accountpin.ErrInvalidMode: http.StatusBadRequest,
		accountpin.ErrInvalidPin:  http.StatusBadRequest,
		accountpin.ErrInvalidCode: http.StatusBadRequest,
		errBoom:                   http.StatusInternalServerError,
	} {
		e.pins.EXPECT().UpdateAccountPin(gomock.Any(), gomock.Any()).Return(nil, err)
		res, _ = e.do(t, http.MethodPut, "/api/v1/users/me/pin", `{"mode":"off"}`)
		assert.Equal(t, status, res.StatusCode, err.Error())
	}
	e.pins.EXPECT().GetAccountPin(gomock.Any(), gomock.Any()).Return(nil, errBoom)
	res, _ = e.do(t, http.MethodGet, "/api/v1/users/me/pin", "")
	assert.Equal(t, http.StatusInternalServerError, res.StatusCode)
	e.pins.EXPECT().NewTOTP(gomock.Any(), gomock.Any()).Return(nil, errBoom)
	res, _ = e.do(t, http.MethodPost, "/api/v1/users/me/pin/totp", "")
	assert.Equal(t, http.StatusInternalServerError, res.StatusCode)
}

func TestAccountPin_MissingUserID(t *testing.T) {
	app := fiber.New()
	h := &AccountPin{}
	app.Get("/pin", h.Get)
	app.Put("/pin", h.Update)
	app.Post("/totp", h.NewTOTP)
	for _, r := range [][2]string{{http.MethodGet, "/pin"}, {http.MethodPut, "/pin"}, {http.MethodPost, "/totp"}} {
		res, _ := do(t, app, r[0], r[1])
		assert.Equal(t, http.StatusBadRequest, res.StatusCode, r[1])
	}
}
