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

func TestFromHTTPRequestToListCreditActivitiesRequestEntity(t *testing.T) {
	cases := map[string]struct {
		target string
		local  any
		want   *entity.ListCreditActivitiesRequest
	}{
		"first page":    {target: "/", local: int64(9), want: &entity.ListCreditActivitiesRequest{UserID: 9}},
		"cursor, limit": {target: "/?before=" + FromIDToBase62(42) + "&limit=20", local: int64(9), want: &entity.ListCreditActivitiesRequest{UserID: 9, Before: 42, Limit: 20}},
		"signed out":    {target: "/"},
		"zero cursor":   {target: "/?before=0", local: int64(9)},
		"bad limit":     {target: "/?limit=x", local: int64(9)},
		"zero limit":    {target: "/?limit=0", local: int64(9)},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var got *entity.ListCreditActivitiesRequest
			run(t, "/", httptest.NewRequest(http.MethodGet, tc.target, nil), func(c fiber.Ctx) {
				if tc.local != nil {
					c.Locals(LocalUserID, tc.local)
				}
				got = FromHTTPRequestToListCreditActivitiesRequestEntity(c)
			})
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestFromListCreditActivitiesResponseEntityToHTTPResponse(t *testing.T) {
	at := time.Date(2026, 10, 19, 4, 0, 0, 0, time.FixedZone("x", 3600))
	got := FromListCreditActivitiesResponseEntityToHTTPResponse(&entity.ListCreditActivitiesResponse{
		Activities: []entity.CreditActivity{{ID: 62, Ops: "spend", Change: -3, Current: 247, CreatedAt: at}},
		Next:       62,
	})
	assert.JSONEq(t, `{"activities":[{"id":"00000000010","ops":"spend","change":-3,"current":247,"createdAt":"2026-10-19T03:00:00Z"}],"next":"00000000010"}`, string(got))

	got = FromListCreditActivitiesResponseEntityToHTTPResponse(&entity.ListCreditActivitiesResponse{})
	assert.JSONEq(t, `{"activities":[],"next":""}`, string(got))
}
