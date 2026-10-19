// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package model

import (
	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/data/model"
)

// _creditOps names the stored credit operations.
var _creditOps = map[uint8]string{
	model.CreditOpGrant:      "grant",
	model.CreditOpRenewal:    "renewal",
	model.CreditOpSpend:      "spend",
	model.CreditOpRefund:     "refund",
	model.CreditOpAdminGrant: "admin_grant",
	model.CreditOpExpiry:     "expiry",
}

// FromCreditOpToCreditOpName names a stored credit operation; an unknown
// one is "unknown".
func FromCreditOpToCreditOpName(op uint8) string {
	if name, ok := _creditOps[op]; ok {
		return name
	}
	return "unknown"
}

// FromCreditActivityModelsToCreditActivityEntities maps stored credit
// activities to domain ones.
func FromCreditActivityModelsToCreditActivityEntities(ms []model.CreditActivity) []entity.CreditActivity {
	es := make([]entity.CreditActivity, len(ms))
	for i, m := range ms {
		es[i] = entity.CreditActivity{
			ID:        m.ID,
			Ops:       FromCreditOpToCreditOpName(m.Ops),
			Change:    m.Change,
			Current:   m.Current,
			CreatedAt: m.CreatedAt,
		}
	}
	return es
}
