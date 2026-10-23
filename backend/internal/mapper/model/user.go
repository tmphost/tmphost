// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package model maps between database models and domain entities.
package model

import (
	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/data/model"
)

// CreditsScale is the balance the credit meter shows as full. A balance
// above it raises the meter's top to the next multiple of it.
const CreditsScale = 10000

// FromUserModelToUserEntity maps a stored user to the domain user.
// monthlyGrant is policy rather than data.
func FromUserModelToUserEntity(m model.User, monthlyGrant int) entity.User {
	return entity.User{
		ID:             m.ID,
		Email:          m.Email,
		Name:           m.Name,
		Picture:        m.Picture,
		Provider:       m.Provider,
		Credits:        m.Credits,
		CreditsLimit:   creditsLimit(m.Credits),
		MonthlyGrant:   monthlyGrant,
		CreditsRenewAt: m.CreditsRenewAt,
		CreatedAt:      m.CreatedAt,
	}
}

// creditsLimit is the top of the credit meter: CreditsScale, or the next
// multiple of it at or above the balance.
func creditsLimit(credits int) int {
	return max(1, (credits+CreditsScale-1)/CreditsScale) * CreditsScale
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
