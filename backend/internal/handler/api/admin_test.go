// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"

	mockauth "github.com/tmphost/tmphost/backend/internal/controller/auth/mocks"
	creditctrl "github.com/tmphost/tmphost/backend/internal/controller/credit"
	mockcredit "github.com/tmphost/tmphost/backend/internal/controller/credit/mocks"
	mocksetup "github.com/tmphost/tmphost/backend/internal/controller/setup/mocks"
	"github.com/tmphost/tmphost/backend/internal/data/entity"
)

const _grantsPath = "/api/v1/admin/credits/grants"

// newAdminApp is the API with a root session that is valid whenever a
// request carries _rootCookie.
func newAdminApp(t *testing.T) (*fiber.App, *mockcredit.MockController) {
	t.Helper()
	mc := gomock.NewController(t)
	setup := mocksetup.NewMockController(mc)
	setup.EXPECT().Authenticate(gomock.Any(), &entity.AuthenticateRootRequest{SessionToken: "root-session"}).Return(nil).AnyTimes()
	credits := mockcredit.NewMockController(mc)
	app := fiber.New()
	New(Params{Router: app, Auth: mockauth.NewMockController(mc), Setup: setup, Credits: credits, FileSettings: func() entity.FileSettings { return entity.FileSettings{} }})
	return app, credits
}

func TestAdminCreateCreditGrant(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		app, credits := newAdminApp(t)
		credits.EXPECT().GrantCredits(gomock.Any(), &entity.GrantCreditsRequest{Credits: 500, All: true}).
			Return(&entity.GrantCreditsResponse{Users: 3, ExpiresAt: _at}, nil)
		res, body := send(t, app, http.MethodPost, _grantsPath, `{"credits":500,"all":true}`, _rootCookie)
		assert.Equal(t, http.StatusCreated, res.StatusCode)
		assert.JSONEq(t, `{"users":3,"expiresAt":"2026-10-18T12:00:00Z"}`, body)
	})

	t.Run("root only", func(t *testing.T) {
		app, _ := newAdminApp(t)
		res, _ := send(t, app, http.MethodPost, _grantsPath, `{"credits":500,"all":true}`)
		assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
	})

	t.Run("bad body", func(t *testing.T) {
		app, _ := newAdminApp(t)
		res, _ := send(t, app, http.MethodPost, _grantsPath, `nope`, _rootCookie)
		assert.Equal(t, http.StatusBadRequest, res.StatusCode)
	})

	for err, status := range map[error]int{
		fmt.Errorf("%w: credits", creditctrl.ErrInvalidGrant): http.StatusBadRequest,
		creditctrl.ErrUserNotFound:                            http.StatusNotFound,
		errors.New("boom"):                                    http.StatusInternalServerError,
	} {
		t.Run(err.Error(), func(t *testing.T) {
			app, credits := newAdminApp(t)
			credits.EXPECT().GrantCredits(gomock.Any(), &entity.GrantCreditsRequest{Credits: 1, Email: "a@b.c"}).Return(nil, err)
			res, body := send(t, app, http.MethodPost, _grantsPath, `{"credits":1,"email":"a@b.c"}`, _rootCookie)
			assert.Equal(t, status, res.StatusCode)
			assert.NotContains(t, body, "boom")
		})
	}
}
