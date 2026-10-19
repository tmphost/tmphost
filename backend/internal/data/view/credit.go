// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package view

type (
	// CreditActivity is one change to the user's credit balance.
	CreditActivity struct {
		ID string `json:"id"`
		// Ops is grant, renewal, spend, refund, admin_grant or expiry.
		Ops string `json:"ops"`
		// Change is the signed amount: +X added, -Y taken.
		Change int `json:"change"`
		// Current is the balance right after the change.
		Current   int    `json:"current"`
		CreatedAt string `json:"createdAt"`
	}

	// GrantCreditsRequest is root's credit grant: Email names one user,
	// or All grants to every user.
	GrantCreditsRequest struct {
		Credits int    `json:"credits"`
		Email   string `json:"email"`
		All     bool   `json:"all"`
	}

	// GrantCreditsResponse reports a credit grant.
	GrantCreditsResponse struct {
		// Users is how many users got the grant.
		Users     int    `json:"users"`
		ExpiresAt string `json:"expiresAt"`
	}

	// ListCreditActivitiesResponse is a page of credit history, newest
	// first.
	ListCreditActivitiesResponse struct {
		Activities []CreditActivity `json:"activities"`
		// Next is the before cursor of the next page; empty on the last.
		Next string `json:"next"`
	}
)
