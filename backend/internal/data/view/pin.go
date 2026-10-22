// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package view

type (
	// AccountPin is the PIN that guards every folder of the user, and
	// whether folders agents create get PINs of their own while it is off.
	AccountPin struct {
		// Mode is off, static or totp.
		Mode      string `json:"mode"`
		AgentPins bool   `json:"agentPins"`
	}

	// UpdateAccountPinRequest is the PUT /users/me/pin body.
	UpdateAccountPinRequest struct {
		Mode string `json:"mode"`
		// Pin is a new static PIN; empty keeps the current one.
		Pin string `json:"pin,omitempty"`
		// Secret is a new authenticator secret and Code its current code;
		// empty keeps the current one.
		Secret    string `json:"secret,omitempty"`
		Code      string `json:"code,omitempty"`
		AgentPins bool   `json:"agentPins"`
	}

	// TOTP is a new authenticator secret to scan.
	TOTP struct {
		Secret string `json:"secret"`
		// URL is the otpauth:// URL; QRCode holds it as a PNG data URI.
		URL    string `json:"url"`
		QRCode string `json:"qrCode"`
	}
)
