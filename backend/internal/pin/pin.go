// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package pin makes and checks the 6 digit PINs that guard folders: static
// PINs, stored as bcrypt hashes, and authenticator app (TOTP) codes.
package pin

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"image/png"
	"strconv"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
)

const (
	// Length is how many digits a PIN has.
	Length = 6
	// Issuer names tmphost in authenticator apps.
	Issuer = "tmphost.io"

	_qrSize = 256
	// _period is how long a TOTP code lasts; one code either side of the
	// current one is accepted too, for clock drift.
	_period = 30
	_skew   = 1
)

// TOTP is a new authenticator app secret, to scan before it is saved.
type TOTP struct {
	// Secret is the base32 secret.
	Secret string
	// URL is the otpauth:// URL the QR code holds.
	URL string
	// QRCode is the QR code as a PNG data URI.
	QRCode string
}

// Valid reports whether p is a PIN: exactly Length ASCII digits.
func Valid(p string) bool {
	if len(p) != Length {
		return false
	}
	for _, r := range p {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Generate returns a random PIN.
func Generate() string {
	var b [4]byte
	// crypto/rand never fails; the modulo bias is under 0.03%.
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%0*d", Length, binary.BigEndian.Uint32(b[:])%1_000_000)
}

// Hash returns the bcrypt hash of a PIN.
func Hash(p string) string {
	// bcrypt fails only for passwords over 72 bytes.
	h, _ := bcrypt.GenerateFromPassword([]byte(p), bcrypt.DefaultCost)
	return string(h)
}

// Match reports whether p is the PIN hash was made from.
func Match(hash, p string) bool {
	return Valid(p) && bcrypt.CompareHashAndPassword([]byte(hash), []byte(p)) == nil
}

// Fingerprint names a PIN by what it is stored as (its hash, or its
// encrypted TOTP secret), so a token that proves the PIN was entered stops
// working once the PIN changes, without revealing anything about it.
func Fingerprint(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)[:12])
}

// NewTOTP returns a new authenticator app secret for account.
func NewTOTP(account string) (TOTP, error) {
	return newTOTP(account, _qrSize)
}

func newTOTP(account string, qrSize int) (TOTP, error) {
	key, err := totp.Generate(totp.GenerateOpts{Issuer: Issuer, AccountName: account, Period: _period})
	if err != nil {
		return TOTP{}, fmt.Errorf("generate totp: %w", err)
	}
	img, err := key.Image(qrSize, qrSize)
	if err != nil {
		return TOTP{}, fmt.Errorf("totp qr code: %w", err)
	}
	var buf bytes.Buffer
	// Encoding a QR code image into memory cannot fail.
	_ = png.Encode(&buf, img)
	return TOTP{
		Secret: key.Secret(),
		URL:    key.URL(),
		QRCode: "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()),
	}, nil
}

// ValidTOTP reports whether code is the authenticator code of secret at
// now, or one period either side of it.
func ValidTOTP(secret, code string, now time.Time) bool {
	if !Valid(code) {
		return false
	}
	ok, err := totp.ValidateCustom(code, secret, now, totp.ValidateOpts{Period: _period, Skew: _skew, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	return ok && err == nil
}

// Sealer encrypts TOTP secrets at rest; encryption.Service is one.
type Sealer interface {
	Encrypt(plaintext, aad []byte) []byte
	Decrypt(ciphertext, aad []byte) ([]byte, error)
}

// SealSecret encrypts a user's TOTP secret for the database.
func SealSecret(s Sealer, userID int64, secret string) string {
	return base64.StdEncoding.EncodeToString(s.Encrypt([]byte(secret), secretAAD(userID)))
}

// OpenSecret decrypts a TOTP secret SealSecret sealed for the user.
func OpenSecret(s Sealer, userID int64, sealed string) (string, error) {
	ct, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return "", fmt.Errorf("decode totp secret: %w", err)
	}
	secret, err := s.Decrypt(ct, secretAAD(userID))
	if err != nil {
		return "", fmt.Errorf("decrypt totp secret: %w", err)
	}
	return string(secret), nil
}

// secretAAD binds a sealed secret to its user, so it can't be moved to
// another account.
func secretAAD(userID int64) []byte {
	return []byte("totp:" + strconv.FormatInt(userID, 10))
}
