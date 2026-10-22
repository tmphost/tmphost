// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package accountpin

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/data/model"
	"github.com/tmphost/tmphost/backend/internal/pin"
	"github.com/tmphost/tmphost/backend/internal/repository"
	mockrepo "github.com/tmphost/tmphost/backend/internal/repository/mocks"
	"github.com/tmphost/tmphost/backend/internal/service/encryption"
)

var (
	_now    = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	errBoom = errors.New("boom")
)

type env struct {
	ctrl    Controller
	users   *mockrepo.MockUserRepository
	secrets encryption.Service
}

func newEnv(t *testing.T) *env {
	t.Helper()
	secrets, err := encryption.New(strings.Repeat("k", 32))
	require.NoError(t, err)
	e := &env{users: mockrepo.NewMockUserRepository(gomock.NewController(t)), secrets: secrets}
	e.ctrl = New(Params{Users: e.users, Secrets: secrets, Now: func() time.Time { return _now }})
	return e
}

func (e *env) user(u model.User) {
	u.ID, u.Email = 7, "me@example.com"
	e.users.EXPECT().FindUserByID(gomock.Any(), int64(7)).Return(u, nil)
}

func TestNew_DefaultClock(t *testing.T) {
	c := New(Params{}).(*controller)
	assert.WithinDuration(t, time.Now(), c.now(), time.Minute)
}

func TestGetAccountPin(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.user(model.User{PinMode: model.PinModeTOTP, AgentPins: true})
	rs, err := e.ctrl.GetAccountPin(ctx, &entity.GetAccountPinRequest{UserID: 7})
	require.NoError(t, err)
	assert.Equal(t, entity.AccountPin{Mode: entity.PinModeTOTP, AgentPins: true}, rs.Pin)

	e.users.EXPECT().FindUserByID(ctx, int64(7)).Return(model.User{}, repository.ErrNotFound)
	_, err = e.ctrl.GetAccountPin(ctx, &entity.GetAccountPinRequest{UserID: 7})
	assert.ErrorIs(t, err, ErrNotFound)

	e.users.EXPECT().FindUserByID(ctx, int64(7)).Return(model.User{}, errBoom)
	_, err = e.ctrl.GetAccountPin(ctx, &entity.GetAccountPinRequest{UserID: 7})
	assert.ErrorIs(t, err, errBoom)
}

func TestNewTOTP(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.user(model.User{})
	rs, err := e.ctrl.NewTOTP(ctx, &entity.NewTOTPRequest{UserID: 7})
	require.NoError(t, err)
	assert.NotEmpty(t, rs.Secret)
	assert.Contains(t, rs.URL, "otpauth://totp/tmphost.io:me@example.com")
	assert.True(t, strings.HasPrefix(rs.QRCode, "data:image/png;base64,"))

	e.users.EXPECT().FindUserByID(ctx, int64(7)).Return(model.User{}, errBoom)
	_, err = e.ctrl.NewTOTP(ctx, &entity.NewTOTPRequest{UserID: 7})
	assert.ErrorIs(t, err, errBoom)

	// An account with no email can't be named in an authenticator app.
	e.users.EXPECT().FindUserByID(ctx, int64(7)).Return(model.User{ID: 7}, nil)
	_, err = e.ctrl.NewTOTP(ctx, &entity.NewTOTPRequest{UserID: 7})
	assert.Error(t, err)
}

func TestUpdateAccountPin_Static(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)

	e.user(model.User{})
	var hash string
	e.users.EXPECT().UpdateUserPin(ctx, int64(7), model.PinModeStatic, gomock.Any(), "", true).DoAndReturn(
		func(_ context.Context, _ int64, _ uint8, h, _ string, _ bool) error {
			hash = h
			return nil
		})
	rs, err := e.ctrl.UpdateAccountPin(ctx, &entity.UpdateAccountPinRequest{UserID: 7, Mode: entity.PinModeStatic, Pin: "123456", AgentPins: true})
	require.NoError(t, err)
	assert.Equal(t, entity.AccountPin{Mode: entity.PinModeStatic, AgentPins: true}, rs.Pin)
	assert.True(t, pin.Match(hash, "123456"))

	// No PIN keeps the current one.
	e.user(model.User{PinMode: model.PinModeStatic, PinHash: hash, AgentPins: true})
	e.users.EXPECT().UpdateUserPin(ctx, int64(7), model.PinModeStatic, hash, "", false).Return(nil)
	_, err = e.ctrl.UpdateAccountPin(ctx, &entity.UpdateAccountPinRequest{UserID: 7, Mode: entity.PinModeStatic})
	require.NoError(t, err)

	// But there must be one.
	e.user(model.User{})
	_, err = e.ctrl.UpdateAccountPin(ctx, &entity.UpdateAccountPinRequest{UserID: 7, Mode: entity.PinModeStatic})
	assert.ErrorIs(t, err, ErrInvalidPin)
	e.user(model.User{})
	_, err = e.ctrl.UpdateAccountPin(ctx, &entity.UpdateAccountPinRequest{UserID: 7, Mode: entity.PinModeStatic, Pin: "12345a"})
	assert.ErrorIs(t, err, ErrInvalidPin)
}

func TestUpdateAccountPin_TOTP(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	key, err := pin.NewTOTP("me@example.com")
	require.NoError(t, err)
	code, err := totp.GenerateCode(key.Secret, _now)
	require.NoError(t, err)

	e.user(model.User{PinMode: model.PinModeStatic, PinHash: "h"})
	var sealed string
	e.users.EXPECT().UpdateUserPin(ctx, int64(7), model.PinModeTOTP, "", gomock.Any(), false).DoAndReturn(
		func(_ context.Context, _ int64, _ uint8, _, s string, _ bool) error {
			sealed = s
			return nil
		})
	rs, err := e.ctrl.UpdateAccountPin(ctx, &entity.UpdateAccountPinRequest{UserID: 7, Mode: entity.PinModeTOTP, Secret: key.Secret, Code: code})
	require.NoError(t, err)
	assert.Equal(t, entity.PinModeTOTP, rs.Pin.Mode)
	secret, err := pin.OpenSecret(e.secrets, 7, sealed)
	require.NoError(t, err)
	assert.Equal(t, key.Secret, secret, "stored encrypted")

	// No secret keeps the current one.
	e.user(model.User{PinMode: model.PinModeTOTP, TOTPSecret: sealed})
	e.users.EXPECT().UpdateUserPin(ctx, int64(7), model.PinModeTOTP, "", sealed, true).Return(nil)
	_, err = e.ctrl.UpdateAccountPin(ctx, &entity.UpdateAccountPinRequest{UserID: 7, Mode: entity.PinModeTOTP, AgentPins: true})
	require.NoError(t, err)

	// A new secret needs its code; none at all needs one too.
	e.user(model.User{})
	_, err = e.ctrl.UpdateAccountPin(ctx, &entity.UpdateAccountPinRequest{UserID: 7, Mode: entity.PinModeTOTP, Secret: key.Secret, Code: "000000"})
	assert.ErrorIs(t, err, ErrInvalidCode)
	e.user(model.User{})
	_, err = e.ctrl.UpdateAccountPin(ctx, &entity.UpdateAccountPinRequest{UserID: 7, Mode: entity.PinModeTOTP})
	assert.ErrorIs(t, err, ErrInvalidCode)
}

func TestUpdateAccountPin_Off(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.user(model.User{PinMode: model.PinModeTOTP, TOTPSecret: "s", PinHash: "h"})
	e.users.EXPECT().UpdateUserPin(ctx, int64(7), model.PinModeOff, "", "", true).Return(nil)
	rs, err := e.ctrl.UpdateAccountPin(ctx, &entity.UpdateAccountPinRequest{UserID: 7, Mode: entity.PinModeOff, AgentPins: true})
	require.NoError(t, err)
	assert.Equal(t, entity.AccountPin{Mode: entity.PinModeOff, AgentPins: true}, rs.Pin)
}

func TestUpdateAccountPin_Errors(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	_, err := e.ctrl.UpdateAccountPin(ctx, &entity.UpdateAccountPinRequest{UserID: 7, Mode: entity.PinModeFolder})
	assert.ErrorIs(t, err, ErrInvalidMode)

	e.users.EXPECT().FindUserByID(ctx, int64(7)).Return(model.User{}, repository.ErrNotFound)
	_, err = e.ctrl.UpdateAccountPin(ctx, &entity.UpdateAccountPinRequest{UserID: 7, Mode: entity.PinModeOff})
	assert.ErrorIs(t, err, ErrNotFound)

	e.user(model.User{})
	e.users.EXPECT().UpdateUserPin(ctx, int64(7), model.PinModeOff, "", "", false).Return(repository.ErrNotFound)
	_, err = e.ctrl.UpdateAccountPin(ctx, &entity.UpdateAccountPinRequest{UserID: 7, Mode: entity.PinModeOff})
	assert.ErrorIs(t, err, ErrNotFound)

	e.user(model.User{})
	e.users.EXPECT().UpdateUserPin(ctx, int64(7), model.PinModeOff, "", "", false).Return(errBoom)
	_, err = e.ctrl.UpdateAccountPin(ctx, &entity.UpdateAccountPinRequest{UserID: 7, Mode: entity.PinModeOff})
	assert.ErrorIs(t, err, errBoom)
}
