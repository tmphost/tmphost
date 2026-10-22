// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/data/model"
)

func TestFromUserModelToUserEntity(t *testing.T) {
	now := time.Now()
	got := FromUserModelToUserEntity(model.User{
		ID: 7, Email: "a@b.c", Name: "A", Picture: "p", Provider: "google",
		Credits: 12, CreditsRenewAt: now, CreatedAt: now,
	}, 250)
	assert.Equal(t, entity.User{
		ID: 7, Email: "a@b.c", Name: "A", Picture: "p", Provider: "google",
		Credits: 12, CreditsLimit: 250, MonthlyGrant: 250, CreditsRenewAt: now, CreatedAt: now,
	}, got)
}

func TestFromUserModelToUserEntity_SignupBalanceAboveGrant(t *testing.T) {
	got := FromUserModelToUserEntity(model.User{Credits: 1000}, 250)
	assert.Equal(t, 1000, got.CreditsLimit)
	assert.Equal(t, 250, got.MonthlyGrant)
}

func TestFromUserModelToAccountPinEntity(t *testing.T) {
	assert.Equal(t, entity.AccountPin{Mode: entity.PinModeOff}, FromUserModelToAccountPinEntity(model.User{}))
	assert.Equal(t, entity.AccountPin{Mode: entity.PinModeStatic, AgentPins: true}, FromUserModelToAccountPinEntity(model.User{PinMode: model.PinModeStatic, AgentPins: true}))
	assert.Equal(t, entity.AccountPin{Mode: entity.PinModeTOTP}, FromUserModelToAccountPinEntity(model.User{PinMode: model.PinModeTOTP}))
	assert.Equal(t, entity.AccountPin{Mode: entity.PinModeOff}, FromUserModelToAccountPinEntity(model.User{PinMode: 99}), "unknown reads as off")
}

func TestFromPinModeEntityToPinModeModel(t *testing.T) {
	for name, want := range map[string]uint8{entity.PinModeOff: model.PinModeOff, entity.PinModeStatic: model.PinModeStatic, entity.PinModeTOTP: model.PinModeTOTP} {
		got := FromPinModeEntityToPinModeModel(name)
		if assert.NotNil(t, got, name) {
			assert.Equal(t, want, *got, name)
		}
	}
	assert.Nil(t, FromPinModeEntityToPinModeModel(entity.PinModeFolder), "not an account mode")
	assert.Nil(t, FromPinModeEntityToPinModeModel(""))
}
