// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package model

import "time"

// Credit activity operations, as stored. Zero is never stored.
const (
	// CreditOpGrant is the grant a new account starts with.
	CreditOpGrant uint8 = iota + 1
	// CreditOpRenewal resets the balance to the grant.
	CreditOpRenewal
	// CreditOpSpend pays for file writes.
	CreditOpSpend
	// CreditOpRefund gives back what failed file writes were paid with.
	CreditOpRefund
	// CreditOpAdminGrant adds credits root granted; they expire.
	CreditOpAdminGrant
	// CreditOpExpiry takes away what is left of an expired admin grant.
	CreditOpExpiry
)

// CreditActivity is one change to a user's credit balance. It is written
// in the same transaction as the change, so a user's history always ends
// at the current balance. Its ID is a monoflake ID, so IDs follow the
// order activities happened in.
type CreditActivity struct {
	ID        int64     `gorm:"primaryKey;autoIncrement:false"`
	CreatedAt time.Time `gorm:"not null"`
	UserID    int64     `gorm:"index;not null"`
	// Ops is what changed the balance: one of the CreditOp* constants.
	Ops uint8 `gorm:"not null"`
	// Change is the signed amount: +X added, -Y taken.
	Change int `gorm:"not null"`
	// Current is the balance right after the change.
	Current int `gorm:"not null"`
	// Agent names what made the change: the OAuth client's name (e.g.
	// "Claude Code", "tmphost CLI") or "Web". Empty for changes the system
	// makes: grants, renewals and expiries.
	Agent string `gorm:"type:varchar(100);not null;default:''"`
}
