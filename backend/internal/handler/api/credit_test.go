// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"

	"github.com/tmphost/tmphost/backend/internal/data/entity"
)

func TestCreditList(t *testing.T) {
	const path = "/api/v1/users/me/credits/activities"

	t.Run("ok", func(t *testing.T) {
		e := newFilesApp(t)
		e.credits.EXPECT().ListCreditActivities(gomock.Any(), &entity.ListCreditActivitiesRequest{UserID: 7, Limit: 2}).Return(&entity.ListCreditActivitiesResponse{
			Activities: []entity.CreditActivity{{ID: 3, Ops: "renewal", Change: 240, Current: 250, CreatedAt: _at}},
		}, nil)
		res, body := e.do(t, http.MethodGet, path+"?limit=2", "")
		assert.Equal(t, http.StatusOK, res.StatusCode)
		assert.JSONEq(t, `{"activities":[{"id":"00000000003","ops":"renewal","change":240,"current":250,"agent":"","createdAt":"2026-10-18T12:00:00Z"}],"next":""}`, body)
	})

	t.Run("signed out", func(t *testing.T) {
		e := newFilesApp(t)
		res, _ := do(t, e.app, http.MethodGet, path)
		assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
	})

	t.Run("bad query", func(t *testing.T) {
		e := newFilesApp(t)
		res, _ := e.do(t, http.MethodGet, path+"?limit=x", "")
		assert.Equal(t, http.StatusBadRequest, res.StatusCode)
	})

	t.Run("server error", func(t *testing.T) {
		e := newFilesApp(t)
		e.credits.EXPECT().ListCreditActivities(gomock.Any(), gomock.Any()).Return(nil, errBoom)
		res, body := e.do(t, http.MethodGet, path, "")
		assert.Equal(t, http.StatusInternalServerError, res.StatusCode)
		assert.JSONEq(t, `{"error":"failed to load credit history"}`, body)
	})
}
