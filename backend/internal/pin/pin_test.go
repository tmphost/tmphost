// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package pin

import (
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tmphost/tmphost/backend/internal/service/encryption"
)

func TestValid(t *testing.T) {
	assert.True(t, Valid("000000"))
	assert.True(t, Valid("123456"))
	assert.False(t, Valid(""))
	assert.False(t, Valid("12345"))
	assert.False(t, Valid("1234567"))
	assert.False(t, Valid("12a456"))
	assert.False(t, Valid("１２３４５"))
}

func TestGenerate(t *testing.T) {
	seen := map[string]bool{}
	for range 20 {
		p := Generate()
		assert.True(t, Valid(p), p)
		seen[p] = true
	}
	assert.Greater(t, len(seen), 1)
}

func TestHashMatch(t *testing.T) {
	h := Hash("123456")
	assert.NotContains(t, h, "123456")
	assert.True(t, Match(h, "123456"))
	assert.False(t, Match(h, "123457"))
	assert.False(t, Match(h, "12345"))
	assert.False(t, Match("not a hash", "123456"))
}

func TestFingerprint(t *testing.T) {
	a := Fingerprint("static", "hash")
	assert.Len(t, a, 24)
	assert.Equal(t, a, Fingerprint("static", "hash"))
	assert.NotEqual(t, a, Fingerprint("static", "hash2"))
	// Parts are separated, so they can't run into each other.
	assert.NotEqual(t, Fingerprint("ab", "c"), Fingerprint("a", "bc"))
}

func TestNewTOTP(t *testing.T) {
	k, err := NewTOTP("me@example.com")
	require.NoError(t, err)
	assert.NotEmpty(t, k.Secret)
	assert.True(t, strings.HasPrefix(k.URL, "otpauth://totp/tmphost.io:me@example.com?"), k.URL)
	assert.Contains(t, k.URL, "secret="+k.Secret)
	assert.True(t, strings.HasPrefix(k.QRCode, "data:image/png;base64,"))

	now := time.Now()
	code, err := totp.GenerateCode(k.Secret, now)
	require.NoError(t, err)
	assert.True(t, ValidTOTP(k.Secret, code, now))
	// One period of drift either way is fine; two is not.
	assert.True(t, ValidTOTP(k.Secret, code, now.Add(30*time.Second)))
	assert.False(t, ValidTOTP(k.Secret, code, now.Add(90*time.Second)))
	assert.False(t, ValidTOTP(k.Secret, "abc", now))
	assert.False(t, ValidTOTP("!!bad", "123456", now))
}

func TestNewTOTP_Errors(t *testing.T) {
	_, err := NewTOTP("")
	assert.Error(t, err)
	_, err = newTOTP("me@example.com", 1)
	assert.Error(t, err)
}

func TestSealSecret(t *testing.T) {
	enc, err := encryption.New(strings.Repeat("k", 32))
	require.NoError(t, err)
	sealed := SealSecret(enc, 7, "JBSWY3DPEHPK3PXP")
	assert.NotContains(t, sealed, "JBSWY3DPEHPK3PXP")
	secret, err := OpenSecret(enc, 7, sealed)
	require.NoError(t, err)
	assert.Equal(t, "JBSWY3DPEHPK3PXP", secret)

	_, err = OpenSecret(enc, 8, sealed)
	assert.Error(t, err, "bound to its user")
	_, err = OpenSecret(enc, 7, "!!")
	assert.Error(t, err)
}
