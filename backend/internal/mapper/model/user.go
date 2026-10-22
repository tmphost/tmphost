// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package model maps between database models and domain entities.
package model

import (
	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/data/model"
)

// FromUserModelToUserEntity maps a stored user to the domain user.
// creditsLimit is the monthly grant, which is policy rather than data; a
// balance above it (the sign-up grant or admin grants) is its own limit.
func FromUserModelToUserEntity(m model.User, creditsLimit int) entity.User {
	return entity.User{
		ID:             m.ID,
		Email:          m.Email,
		Name:           m.Name,
		Picture:        m.Picture,
		Provider:       m.Provider,
		Credits:        m.Credits,
		CreditsLimit:   max(creditsLimit, m.Credits),
		MonthlyGrant:   creditsLimit,
		CreditsRenewAt: m.CreditsRenewAt,
		CreatedAt:      m.CreatedAt,
	}
}

// _pinModes names the account PIN modes.
var _pinModes = map[uint8]string{
	model.PinModeOff:    entity.PinModeOff,
	model.PinModeStatic: entity.PinModeStatic,
	model.PinModeTOTP:   entity.PinModeTOTP,
}

// FromUserModelToAccountPinEntity maps a stored user's PIN settings to
// the domain. An unknown mode reads as off.
func FromUserModelToAccountPinEntity(m model.User) entity.AccountPin {
	mode, ok := _pinModes[m.PinMode]
	if !ok {
		mode = entity.PinModeOff
	}
	return entity.AccountPin{Mode: mode, AgentPins: m.AgentPins}
}

// FromPinModeEntityToPinModeModel maps an account PIN mode to its stored
// value; nil for a mode that is not one.
func FromPinModeEntityToPinModeModel(mode string) *uint8 {
	for m, name := range _pinModes {
		if name == mode {
			return &m
		}
	}
	return nil
}
