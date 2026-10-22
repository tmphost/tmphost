// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package entity

// The PIN modes: what guests enter to open a folder.
const (
	// PinModeOff: no account PIN.
	PinModeOff = "off"
	// PinModeStatic: the owner's account PIN, one they picked.
	PinModeStatic = "static"
	// PinModeTOTP: a code of the owner's authenticator app.
	PinModeTOTP = "totp"
	// PinModeFolder: the folder's own PIN, made for it.
	PinModeFolder = "folder"
)

type (
	// AccountPin is the PIN that guards every folder of a user, and
	// whether folders agents create get PINs of their own while it is off.
	AccountPin struct {
		// Mode is PinModeOff, PinModeStatic or PinModeTOTP.
		Mode      string
		AgentPins bool
	}

	// GetAccountPinRequest asks for a user's account PIN settings.
	GetAccountPinRequest struct {
		UserID int64
	}

	// AccountPinResponse carries a user's account PIN settings.
	AccountPinResponse struct {
		Pin AccountPin
	}

	// NewTOTPRequest asks for a new authenticator app secret to scan.
	NewTOTPRequest struct {
		UserID int64
	}

	// NewTOTPResponse is a new authenticator app secret, not saved yet.
	NewTOTPResponse struct {
		Secret string
		// URL is the otpauth:// URL; QRCode holds it as a PNG data URI.
		URL    string
		QRCode string
	}

	// UpdateAccountPinRequest changes a user's account PIN settings.
	UpdateAccountPinRequest struct {
		UserID int64
		// Mode is PinModeOff, PinModeStatic or PinModeTOTP.
		Mode string
		// Pin is the new static PIN; empty keeps the current one.
		Pin string
		// Secret is a new authenticator secret, and Code its current
		// code, which proves it was scanned; empty keeps the current one.
		Secret    string
		Code      string
		AgentPins bool
	}
)
