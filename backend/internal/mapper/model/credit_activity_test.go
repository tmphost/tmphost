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

func TestFromCreditOpToCreditOpName(t *testing.T) {
	assert.Equal(t, "grant", FromCreditOpToCreditOpName(model.CreditOpGrant))
	assert.Equal(t, "renewal", FromCreditOpToCreditOpName(model.CreditOpRenewal))
	assert.Equal(t, "spend", FromCreditOpToCreditOpName(model.CreditOpSpend))
	assert.Equal(t, "refund", FromCreditOpToCreditOpName(model.CreditOpRefund))
	assert.Equal(t, "admin_grant", FromCreditOpToCreditOpName(model.CreditOpAdminGrant))
	assert.Equal(t, "expiry", FromCreditOpToCreditOpName(model.CreditOpExpiry))
	assert.Equal(t, "unknown", FromCreditOpToCreditOpName(0))
}

func TestFromCreditActivityModelsToCreditActivityEntities(t *testing.T) {
	at := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	got := FromCreditActivityModelsToCreditActivityEntities([]model.CreditActivity{
		{ID: 2, CreatedAt: at, UserID: 7, Ops: model.CreditOpRefund, Change: 4, Current: 9},
	})
	assert.Equal(t, []entity.CreditActivity{{ID: 2, Ops: "refund", Change: 4, Current: 9, CreatedAt: at}}, got)
	assert.Empty(t, FromCreditActivityModelsToCreditActivityEntities(nil))
}
