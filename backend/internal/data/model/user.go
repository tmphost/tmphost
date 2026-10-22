// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package model holds the GORM database models.
package model

import "time"

// User is an account. Accounts are keyed by verified email, so signing in
// with Google and GitHub using the same address reaches the same account
// (and the same credit balance).
type User struct {
	ID        int64 `gorm:"primaryKey;autoIncrement:false"`
	CreatedAt time.Time
	UpdatedAt time.Time
	Email     string `gorm:"type:varchar(255);uniqueIndex;not null"`
	Name      string `gorm:"type:varchar(255)"`
	Picture   string `gorm:"type:text"`
	// Provider is the identity provider the account was registered with.
	Provider string `gorm:"type:varchar(32)"`
	// Credits is the remaining operation balance for the current period.
	Credits int `gorm:"not null;default:0"`
	// CreditsRenewAt is when the daily renewal job next resets the balance
	// to the monthly grant. Always 00:00 UTC.
	CreditsRenewAt time.Time `gorm:"index"`
	// PinMode is the account PIN that guards every folder of the user:
	// PinModeOff, PinModeStatic or PinModeTOTP.
	PinMode uint8 `gorm:"not null;default:0"`
	// PinHash is the bcrypt hash of the static PIN.
	PinHash string `gorm:"type:varchar(100);not null;default:''"`
	// TOTPSecret is the authenticator app secret, encrypted with the
	// master key and base64 encoded.
	TOTPSecret string `gorm:"column:totp_secret;type:text;not null;default:''"`
	// AgentPins gives each folder an agent creates a PIN of its own, while
	// no account PIN is set.
	AgentPins bool `gorm:"not null;default:false"`
}

// The account PIN modes.
const (
	PinModeOff uint8 = iota
	// PinModeStatic guards folders with one PIN the user picked.
	PinModeStatic
	// PinModeTOTP guards folders with the codes of an authenticator app.
	PinModeTOTP
)
