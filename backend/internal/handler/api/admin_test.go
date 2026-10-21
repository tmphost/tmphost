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
	"github.com/mustafaturan/monoflake"
	"github.com/stretchr/testify/assert"

	adminctrl "github.com/tmphost/tmphost/backend/internal/controller/admin"
	mockadmin "github.com/tmphost/tmphost/backend/internal/controller/admin/mocks"
	mockauth "github.com/tmphost/tmphost/backend/internal/controller/auth/mocks"
	creditctrl "github.com/tmphost/tmphost/backend/internal/controller/credit"
	mockcredit "github.com/tmphost/tmphost/backend/internal/controller/credit/mocks"
	mocksetup "github.com/tmphost/tmphost/backend/internal/controller/setup/mocks"
	"github.com/tmphost/tmphost/backend/internal/data/entity"
)

const _grantsPath = "/admin/v1/credits/grants"

// newAdminApp is the API with a root session that is valid whenever a
// request carries _rootCookie.
func newAdminApp(t *testing.T) (*fiber.App, *mockcredit.MockController, *mockadmin.MockController) {
	t.Helper()
	mc := gomock.NewController(t)
	setup := mocksetup.NewMockController(mc)
	setup.EXPECT().Authenticate(gomock.Any(), &entity.AuthenticateRootRequest{SessionToken: "root-session"}).Return(nil).AnyTimes()
	credits := mockcredit.NewMockController(mc)
	admin := mockadmin.NewMockController(mc)
	app := fiber.New()
	New(Params{Router: app, Auth: mockauth.NewMockController(mc), Setup: setup, Credits: credits, Admin: admin, FileSettings: func() entity.FileSettings { return entity.FileSettings{} }})
	return app, credits, admin
}

func TestAdminCreateCreditGrant(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		app, credits, _ := newAdminApp(t)
		credits.EXPECT().GrantCredits(gomock.Any(), &entity.GrantCreditsRequest{Credits: 500, All: true}).
			Return(&entity.GrantCreditsResponse{Users: 3, ExpiresAt: _at}, nil)
		res, body := send(t, app, http.MethodPost, _grantsPath, `{"credits":500,"all":true}`, _rootCookie)
		assert.Equal(t, http.StatusCreated, res.StatusCode)
		assert.JSONEq(t, `{"users":3,"expiresAt":"2026-10-18T12:00:00Z"}`, body)
	})

	t.Run("root only", func(t *testing.T) {
		app, _, _ := newAdminApp(t)
		res, _ := send(t, app, http.MethodPost, _grantsPath, `{"credits":500,"all":true}`)
		assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
	})

	t.Run("bad body", func(t *testing.T) {
		app, _, _ := newAdminApp(t)
		res, _ := send(t, app, http.MethodPost, _grantsPath, `nope`, _rootCookie)
		assert.Equal(t, http.StatusBadRequest, res.StatusCode)
	})

	for err, status := range map[error]int{
		fmt.Errorf("%w: credits", creditctrl.ErrInvalidGrant): http.StatusBadRequest,
		creditctrl.ErrUserNotFound:                            http.StatusNotFound,
		errors.New("boom"):                                    http.StatusInternalServerError,
	} {
		t.Run(err.Error(), func(t *testing.T) {
			app, credits, _ := newAdminApp(t)
			credits.EXPECT().GrantCredits(gomock.Any(), &entity.GrantCreditsRequest{Credits: 1, Email: "a@b.c"}).Return(nil, err)
			res, body := send(t, app, http.MethodPost, _grantsPath, `{"credits":1,"email":"a@b.c"}`, _rootCookie)
			assert.Equal(t, status, res.StatusCode)
			assert.NotContains(t, body, "boom")
		})
	}
}

func TestAdminGetStats(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		app, _, admin := newAdminApp(t)
		admin.EXPECT().GetStats(gomock.Any(), &entity.GetAdminStatsRequest{}).
			Return(&entity.GetAdminStatsResponse{Users: 2, Days: []entity.AdminStatsDay{{Date: "2026-10-21", Used: 3}}}, nil)
		res, body := send(t, app, http.MethodGet, "/admin/v1/stats", "", _rootCookie)
		assert.Equal(t, http.StatusOK, res.StatusCode)
		assert.JSONEq(t, `{"stats":{"users":2,"credits":0,"used":0,"signups":0,"folders":0,"days":[{"date":"2026-10-21","used":3,"signups":0}]}}`, body)
	})
	t.Run("root only", func(t *testing.T) {
		app, _, _ := newAdminApp(t)
		res, _ := send(t, app, http.MethodGet, "/admin/v1/stats", "")
		assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
	})
	t.Run("error", func(t *testing.T) {
		app, _, admin := newAdminApp(t)
		admin.EXPECT().GetStats(gomock.Any(), gomock.Any()).Return(nil, errors.New("boom"))
		res, body := send(t, app, http.MethodGet, "/admin/v1/stats", "", _rootCookie)
		assert.Equal(t, http.StatusInternalServerError, res.StatusCode)
		assert.NotContains(t, body, "boom")
	})
}

func TestAdminListUsers(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		app, _, admin := newAdminApp(t)
		admin.EXPECT().ListUsers(gomock.Any(), &entity.ListAdminUsersRequest{Query: "ann", Limit: 5}).
			Return(&entity.ListAdminUsersResponse{Users: []entity.AdminUser{{User: entity.User{ID: 62, Email: "ann@x", CreditsRenewAt: _at, CreatedAt: _at}, Used: 4}}}, nil)
		res, body := send(t, app, http.MethodGet, "/admin/v1/users?q=ann&limit=5", "", _rootCookie)
		assert.Equal(t, http.StatusOK, res.StatusCode)
		assert.Contains(t, body, `"email":"ann@x"`)
		assert.Contains(t, body, `"used":4`)
	})
	t.Run("root only", func(t *testing.T) {
		app, _, _ := newAdminApp(t)
		res, _ := send(t, app, http.MethodGet, "/admin/v1/users", "")
		assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
	})
	t.Run("bad query", func(t *testing.T) {
		app, _, _ := newAdminApp(t)
		res, _ := send(t, app, http.MethodGet, "/admin/v1/users?limit=x", "", _rootCookie)
		assert.Equal(t, http.StatusBadRequest, res.StatusCode)
	})
	t.Run("error", func(t *testing.T) {
		app, _, admin := newAdminApp(t)
		admin.EXPECT().ListUsers(gomock.Any(), gomock.Any()).Return(nil, errors.New("boom"))
		res, body := send(t, app, http.MethodGet, "/admin/v1/users", "", _rootCookie)
		assert.Equal(t, http.StatusInternalServerError, res.StatusCode)
		assert.NotContains(t, body, "boom")
	})
}

func TestAdminGetUser(t *testing.T) {
	path := "/admin/v1/users/" + monoflake.ID(62).String()
	t.Run("ok", func(t *testing.T) {
		app, _, admin := newAdminApp(t)
		admin.EXPECT().GetUser(gomock.Any(), &entity.GetAdminUserRequest{UserID: 62}).
			Return(&entity.GetAdminUserResponse{User: entity.AdminUser{User: entity.User{ID: 62, Email: "ann@x", CreditsRenewAt: _at, CreatedAt: _at}, Folders: 2}}, nil)
		res, body := send(t, app, http.MethodGet, path, "", _rootCookie)
		assert.Equal(t, http.StatusOK, res.StatusCode)
		assert.Contains(t, body, `"folders":2`)
	})
	t.Run("bad id", func(t *testing.T) {
		app, _, _ := newAdminApp(t)
		res, _ := send(t, app, http.MethodGet, "/admin/v1/users/0", "", _rootCookie)
		assert.Equal(t, http.StatusBadRequest, res.StatusCode)
	})
	for err, status := range map[error]int{
		adminctrl.ErrUserNotFound: http.StatusNotFound,
		errors.New("boom"):        http.StatusInternalServerError,
	} {
		t.Run(err.Error(), func(t *testing.T) {
			app, _, admin := newAdminApp(t)
			admin.EXPECT().GetUser(gomock.Any(), gomock.Any()).Return(nil, err)
			res, body := send(t, app, http.MethodGet, path, "", _rootCookie)
			assert.Equal(t, status, res.StatusCode)
			assert.NotContains(t, body, "boom")
		})
	}
}

func TestAdminListUserCreditActivities(t *testing.T) {
	path := "/admin/v1/users/" + monoflake.ID(62).String() + "/credits/activities"
	t.Run("ok", func(t *testing.T) {
		app, _, admin := newAdminApp(t)
		admin.EXPECT().ListUserCreditActivities(gomock.Any(), &entity.ListCreditActivitiesRequest{UserID: 62, Limit: 3}).
			Return(&entity.ListCreditActivitiesResponse{Activities: []entity.CreditActivity{{ID: 9, Ops: "spend", Change: -1, Current: 4, CreatedAt: _at}}}, nil)
		res, body := send(t, app, http.MethodGet, path+"?limit=3", "", _rootCookie)
		assert.Equal(t, http.StatusOK, res.StatusCode)
		assert.Contains(t, body, `"ops":"spend"`)
	})
	t.Run("bad cursor", func(t *testing.T) {
		app, _, _ := newAdminApp(t)
		res, _ := send(t, app, http.MethodGet, path+"?before=0", "", _rootCookie)
		assert.Equal(t, http.StatusBadRequest, res.StatusCode)
	})
	t.Run("not found", func(t *testing.T) {
		app, _, admin := newAdminApp(t)
		admin.EXPECT().ListUserCreditActivities(gomock.Any(), gomock.Any()).Return(nil, adminctrl.ErrUserNotFound)
		res, _ := send(t, app, http.MethodGet, path, "", _rootCookie)
		assert.Equal(t, http.StatusNotFound, res.StatusCode)
	})
}
