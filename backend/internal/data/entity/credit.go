// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package entity

import "time"

type (
	// RenewDueCreditsResponse reports how many accounts were renewed.
	RenewDueCreditsResponse struct {
		Renewed int
		// Expired is how many admin grants expired.
		Expired int
	}

	// GrantCreditsRequest asks root to grant credits to one user (by email)
	// or to every user.
	GrantCreditsRequest struct {
		Credits int
		// Email names the user; empty with All set.
		Email string
		// All grants to every user.
		All bool
	}

	// GrantCreditsResponse reports a grant.
	GrantCreditsResponse struct {
		// Users is how many users got the grant.
		Users int
		// ExpiresAt is when what is left of the grant is taken away.
		ExpiresAt time.Time
	}

	// CreditActivity is one change to a user's credit balance.
	CreditActivity struct {
		ID int64
		// Ops names what changed the balance: grant, renewal, spend,
		// refund, admin_grant or expiry.
		Ops string
		// Change is the signed amount.
		Change int
		// Current is the balance right after the change.
		Current   int
		CreatedAt time.Time
	}

	// ListCreditActivitiesRequest asks for a page of a user's credit
	// history, newest first.
	ListCreditActivitiesRequest struct {
		UserID int64
		// Before is the ID the page starts below; 0 starts at the newest.
		Before int64
		// Limit is the page size; 0 means the default.
		Limit int
	}

	// ListCreditActivitiesResponse is a page of credit history.
	ListCreditActivitiesResponse struct {
		Activities []CreditActivity
		// Next is the Before of the next page; 0 when this is the last.
		Next int64
	}
)
