// Package entity holds the domain structs passed between layers.
package entity

import "time"

type (
	// User is an account as the domain sees it.
	User struct {
		ID             int64
		Email          string
		Name           string
		Picture        string
		Provider       string
		Credits        int
		CreditsLimit   int
		CreditsRenewAt time.Time
		CreatedAt      time.Time
	}

	// GetCurrentUserRequest asks for the signed-in user.
	GetCurrentUserRequest struct {
		UserID int64
	}

	// GetCurrentUserResponse carries the signed-in user.
	GetCurrentUserResponse struct {
		User User
	}
)
