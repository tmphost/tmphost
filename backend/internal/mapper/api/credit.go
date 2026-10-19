// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/mustafaturan/monoflake"

	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/data/view"
)

// FromHTTPRequestToListCreditActivitiesRequestEntity reads the signed-in
// user and the optional before cursor and limit of a history page.
func FromHTTPRequestToListCreditActivitiesRequestEntity(c fiber.Ctx) *entity.ListCreditActivitiesRequest {
	id := localUserID(c)
	if id == 0 {
		return nil
	}
	rq := &entity.ListCreditActivitiesRequest{UserID: id}
	if s := c.Query("before"); s != "" {
		rq.Before = monoflake.IDFromBase62(s).Int64()
		if rq.Before <= 0 {
			return nil
		}
	}
	if s := c.Query("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n <= 0 {
			return nil
		}
		rq.Limit = n
	}
	return rq
}

// FromListCreditActivitiesResponseEntityToHTTPResponse renders a credit
// history page.
func FromListCreditActivitiesResponseEntityToHTTPResponse(rs *entity.ListCreditActivitiesResponse) []byte {
	out := view.ListCreditActivitiesResponse{Activities: FromCreditActivityEntitiesToCreditActivityViews(rs.Activities)}
	if rs.Next > 0 {
		out.Next = FromIDToBase62(rs.Next)
	}
	return marshal(out)
}

// FromCreditActivityEntitiesToCreditActivityViews maps credit activities
// to their JSON views.
func FromCreditActivityEntitiesToCreditActivityViews(es []entity.CreditActivity) []view.CreditActivity {
	vs := make([]view.CreditActivity, len(es))
	for i, e := range es {
		vs[i] = view.CreditActivity{
			ID:        FromIDToBase62(e.ID),
			Ops:       e.Ops,
			Change:    e.Change,
			Current:   e.Current,
			CreatedAt: e.CreatedAt.UTC().Format(time.RFC3339),
		}
	}
	return vs
}

// FromHTTPRequestToGrantCreditsRequestEntity reads POST
// /admin/credits/grants. Checking the amount and target is the
// controller's job.
func FromHTTPRequestToGrantCreditsRequestEntity(c fiber.Ctx) *entity.GrantCreditsRequest {
	var rq view.GrantCreditsRequest
	if err := json.Unmarshal(c.Body(), &rq); err != nil {
		return nil
	}
	return &entity.GrantCreditsRequest{Credits: rq.Credits, Email: rq.Email, All: rq.All}
}

// FromGrantCreditsResponseEntityToHTTPResponse renders a credit grant.
func FromGrantCreditsResponseEntityToHTTPResponse(rs *entity.GrantCreditsResponse) []byte {
	return marshal(view.GrantCreditsResponse{Users: rs.Users, ExpiresAt: rs.ExpiresAt.UTC().Format(time.RFC3339)})
}
