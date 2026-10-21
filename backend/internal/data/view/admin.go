// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package view

type (
	// AdminStats is the stats over every account.
	AdminStats struct {
		Users   int64 `json:"users"`
		Credits int64 `json:"credits"`
		// Used is the credits spent over Days, net of refunds.
		Used    int64           `json:"used"`
		Signups int64           `json:"signups"`
		Folders int64           `json:"folders"`
		Days    []AdminStatsDay `json:"days"`
	}

	// AdminStatsDay is one UTC day of the admin stats.
	AdminStatsDay struct {
		Date    string `json:"date"`
		Used    int64  `json:"used"`
		Signups int64  `json:"signups"`
	}

	// GetAdminStatsResponse wraps the admin stats.
	GetAdminStatsResponse struct {
		Stats AdminStats `json:"stats"`
	}

	// AdminUser is an account as root sees it.
	AdminUser struct {
		User
		// Used is the credits the user spent over the stats days, net of
		// refunds.
		Used int `json:"used"`
		// Folders is how many live folders the user has.
		Folders int `json:"folders"`
	}

	// ListAdminUsersResponse is a page of accounts, newest first.
	ListAdminUsersResponse struct {
		Users []AdminUser `json:"users"`
		// Next is the before cursor of the next page; empty on the last.
		Next string `json:"next"`
	}

	// GetAdminUserResponse wraps one account.
	GetAdminUserResponse struct {
		User AdminUser `json:"user"`
	}
)
