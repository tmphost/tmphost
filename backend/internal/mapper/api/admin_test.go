// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"

	"github.com/tmphost/tmphost/backend/internal/data/entity"
)

func TestFromHTTPRequestToGetAdminStatsRequestEntity(t *testing.T) {
	assert.Equal(t, &entity.GetAdminStatsRequest{}, FromHTTPRequestToGetAdminStatsRequestEntity(nil))
}

func TestFromGetAdminStatsResponseEntityToHTTPResponse(t *testing.T) {
	got := FromGetAdminStatsResponseEntityToHTTPResponse(&entity.GetAdminStatsResponse{
		Users: 3, Credits: 157, Used: 9, Signups: 2, Folders: 4,
		Days: []entity.AdminStatsDay{{Date: "2026-10-21", Used: 9, Signups: 2}},
	})
	assert.JSONEq(t, `{"stats":{"users":3,"credits":157,"used":9,"signups":2,"folders":4,"days":[{"date":"2026-10-21","used":9,"signups":2}]}}`, string(got))
}

func TestFromHTTPRequestToListAdminUsersRequestEntity(t *testing.T) {
	cases := map[string]*entity.ListAdminUsersRequest{
		"/":                              {},
		"/?q=ann&limit=5":                {Query: "ann", Limit: 5},
		"/?before=" + FromIDToBase62(42): {Before: 42},
		"/?limit=x":                      nil,
	}
	for target, want := range cases {
		var got *entity.ListAdminUsersRequest
		run(t, "/", httptest.NewRequest(http.MethodGet, target, nil), func(c fiber.Ctx) {
			got = FromHTTPRequestToListAdminUsersRequestEntity(c)
		})
		assert.Equal(t, want, got, target)
	}
}

func TestFromListAdminUsersResponseEntityToHTTPResponse(t *testing.T) {
	at := time.Date(2026, 10, 19, 4, 0, 0, 0, time.UTC)
	got := FromListAdminUsersResponseEntityToHTTPResponse(&entity.ListAdminUsersResponse{
		Users: []entity.AdminUser{{User: entity.User{ID: 62, Email: "a@b.c", Credits: 5, CreditsLimit: 250, MonthlyGrant: 250, CreditsRenewAt: at, CreatedAt: at}, Used: 7, Folders: 2}},
		Next:  62,
	})
	assert.JSONEq(t, `{"users":[{"id":"00000000010","email":"a@b.c","name":"","picture":"","provider":"","credits":5,"creditsLimit":250,"monthlyGrant":250,"creditsRenewAt":"2026-10-19T04:00:00Z","createdAt":"2026-10-19T04:00:00Z","used":7,"folders":2}],"next":"00000000010"}`, string(got))

	got = FromListAdminUsersResponseEntityToHTTPResponse(&entity.ListAdminUsersResponse{})
	assert.JSONEq(t, `{"users":[],"next":""}`, string(got))
}

func TestFromHTTPRequestToGetAdminUserRequestEntity(t *testing.T) {
	cases := map[string]*entity.GetAdminUserRequest{
		"/users/" + FromIDToBase62(42): {UserID: 42},
		"/users/0":                     nil,
	}
	for target, want := range cases {
		var got *entity.GetAdminUserRequest
		run(t, "/users/:id", httptest.NewRequest(http.MethodGet, target, nil), func(c fiber.Ctx) {
			got = FromHTTPRequestToGetAdminUserRequestEntity(c)
		})
		assert.Equal(t, want, got, target)
	}
}

func TestFromGetAdminUserResponseEntityToHTTPResponse(t *testing.T) {
	at := time.Date(2026, 10, 19, 4, 0, 0, 0, time.UTC)
	got := FromGetAdminUserResponseEntityToHTTPResponse(&entity.GetAdminUserResponse{User: entity.AdminUser{User: entity.User{ID: 62, CreditsRenewAt: at, CreatedAt: at}, Used: 1}})
	assert.JSONEq(t, `{"user":{"id":"00000000010","email":"","name":"","picture":"","provider":"","credits":0,"creditsLimit":0,"monthlyGrant":0,"creditsRenewAt":"2026-10-19T04:00:00Z","createdAt":"2026-10-19T04:00:00Z","used":1,"folders":0}}`, string(got))
}

func TestFromHTTPRequestToListAdminUserCreditActivitiesRequestEntity(t *testing.T) {
	id := FromIDToBase62(42)
	cases := map[string]*entity.ListCreditActivitiesRequest{
		"/users/" + id + "/a": {UserID: 42},
		"/users/" + id + "/a?before=" + FromIDToBase62(9) + "&limit=3": {UserID: 42, Before: 9, Limit: 3},
		"/users/0/a":                   nil,
		"/users/" + id + "/a?before=0": nil,
	}
	for target, want := range cases {
		var got *entity.ListCreditActivitiesRequest
		run(t, "/users/:id/a", httptest.NewRequest(http.MethodGet, target, nil), func(c fiber.Ctx) {
			got = FromHTTPRequestToListAdminUserCreditActivitiesRequestEntity(c)
		})
		assert.Equal(t, want, got, target)
	}
}
