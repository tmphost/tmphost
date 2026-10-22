// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package accountpin implements a user's account PIN: one PIN that
// guards every folder of theirs, either one they pick (static) or the
// codes of an authenticator app (TOTP). While it is off, they can have
// each folder an agent creates get a PIN of its own instead.
package accountpin

//go:generate mockgen -source=accountpin.go -destination=mocks/accountpin.go -package=mocks

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/data/model"
	mappermodel "github.com/tmphost/tmphost/backend/internal/mapper/model"
	"github.com/tmphost/tmphost/backend/internal/pin"
	"github.com/tmphost/tmphost/backend/internal/repository"
)

var (
	// ErrNotFound is returned for an unknown user.
	ErrNotFound = errors.New("user not found")
	// ErrInvalidMode is returned for a mode that is not off, static or
	// totp.
	ErrInvalidMode = errors.New("mode must be off, static or totp")
	// ErrInvalidPin is returned for a static PIN that is not 6 digits.
	ErrInvalidPin = errors.New("pin must be 6 digits")
	// ErrInvalidCode is returned when the code does not match the new
	// authenticator secret.
	ErrInvalidCode = errors.New("the code does not match the authenticator secret")
)

type (
	// Controller reads and changes account PINs.
	Controller interface {
		GetAccountPin(ctx context.Context, rq *entity.GetAccountPinRequest) (*entity.AccountPinResponse, error)
		// NewTOTP makes a new authenticator secret to scan. Nothing is
		// saved until UpdateAccountPin gets it back with a code.
		NewTOTP(ctx context.Context, rq *entity.NewTOTPRequest) (*entity.NewTOTPResponse, error)
		// UpdateAccountPin changes the account PIN. A new static PIN must
		// be 6 digits, and a new authenticator secret comes with its
		// current code, which proves it was scanned.
		UpdateAccountPin(ctx context.Context, rq *entity.UpdateAccountPinRequest) (*entity.AccountPinResponse, error)
	}

	// Params are the controller's dependencies.
	Params struct {
		Users repository.UserRepository
		// Secrets encrypts authenticator secrets at rest.
		Secrets pin.Sealer
		Now     func() time.Time
	}

	controller struct {
		users   repository.UserRepository
		secrets pin.Sealer
		now     func() time.Time
	}
)

// New returns the account PIN controller.
func New(p Params) Controller {
	c := &controller{users: p.Users, secrets: p.Secrets, now: p.Now}
	if c.now == nil {
		c.now = time.Now
	}
	return c
}

func (c *controller) GetAccountPin(ctx context.Context, rq *entity.GetAccountPinRequest) (*entity.AccountPinResponse, error) {
	u, err := c.user(ctx, rq.UserID)
	if err != nil {
		return nil, err
	}
	return &entity.AccountPinResponse{Pin: mappermodel.FromUserModelToAccountPinEntity(u)}, nil
}

func (c *controller) NewTOTP(ctx context.Context, rq *entity.NewTOTPRequest) (*entity.NewTOTPResponse, error) {
	u, err := c.user(ctx, rq.UserID)
	if err != nil {
		return nil, err
	}
	key, err := pin.NewTOTP(u.Email)
	if err != nil {
		return nil, err
	}
	return &entity.NewTOTPResponse{Secret: key.Secret, URL: key.URL, QRCode: key.QRCode}, nil
}

func (c *controller) UpdateAccountPin(ctx context.Context, rq *entity.UpdateAccountPinRequest) (*entity.AccountPinResponse, error) {
	mode := mappermodel.FromPinModeEntityToPinModeModel(rq.Mode)
	if mode == nil {
		return nil, ErrInvalidMode
	}
	u, err := c.user(ctx, rq.UserID)
	if err != nil {
		return nil, err
	}
	var hash, secret string
	switch *mode {
	case model.PinModeStatic:
		switch {
		case rq.Pin != "":
			if !pin.Valid(rq.Pin) {
				return nil, ErrInvalidPin
			}
			hash = pin.Hash(rq.Pin)
		case u.PinMode == model.PinModeStatic:
			hash = u.PinHash
		default:
			return nil, ErrInvalidPin
		}
	case model.PinModeTOTP:
		switch {
		case rq.Secret != "":
			if !pin.ValidTOTP(rq.Secret, rq.Code, c.now()) {
				return nil, ErrInvalidCode
			}
			secret = pin.SealSecret(c.secrets, u.ID, rq.Secret)
		case u.PinMode == model.PinModeTOTP:
			secret = u.TOTPSecret
		default:
			return nil, ErrInvalidCode
		}
	}
	if err := c.users.UpdateUserPin(ctx, u.ID, *mode, hash, secret, rq.AgentPins); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("update account pin: %w", err)
	}
	u.PinMode, u.PinHash, u.TOTPSecret, u.AgentPins = *mode, hash, secret, rq.AgentPins
	return &entity.AccountPinResponse{Pin: mappermodel.FromUserModelToAccountPinEntity(u)}, nil
}

func (c *controller) user(ctx context.Context, id int64) (model.User, error) {
	u, err := c.users.FindUserByID(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return model.User{}, ErrNotFound
	}
	if err != nil {
		return model.User{}, fmt.Errorf("find user: %w", err)
	}
	return u, nil
}
