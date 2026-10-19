// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package view

type (
	// CreditActivity is one change to the user's credit balance.
	CreditActivity struct {
		ID string `json:"id"`
		// Ops is grant, renewal, spend or refund.
		Ops string `json:"ops"`
		// Change is the signed amount: +X added, -Y taken.
		Change int `json:"change"`
		// Current is the balance right after the change.
		Current   int    `json:"current"`
		CreatedAt string `json:"createdAt"`
	}

	// ListCreditActivitiesResponse is a page of credit history, newest
	// first.
	ListCreditActivitiesResponse struct {
		Activities []CreditActivity `json:"activities"`
		// Next is the before cursor of the next page; empty on the last.
		Next string `json:"next"`
	}
)
