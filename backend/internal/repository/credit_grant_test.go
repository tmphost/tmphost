// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package repository

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/tmphost/tmphost/backend/internal/data/model"
	"github.com/tmphost/tmphost/backend/internal/service/idgen"
)

var _day = time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)

// _period is the monthly credit period.
const _period = 30 * 24 * time.Hour

// failCreditsExpr makes every update that sets credits to an expression
// containing sql fail.
func failCreditsExpr(t *testing.T, db *gorm.DB, sql string) {
	t.Helper()
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:fail-credits", func(tx *gorm.DB) {
		if m, ok := tx.Statement.Dest.(map[string]any); ok {
			if e, ok := m["credits"].(clause.Expr); ok && strings.Contains(e.SQL, sql) {
				_ = tx.AddError(errors.New("update failed"))
			}
		}
	}))
}

// newGrantRepo returns a repository with user 1, whose monthly credits
// renew on renewAt.
func newGrantRepo(t *testing.T, credits int, renewAt time.Time) (*gorm.DB, UserRepository) {
	t.Helper()
	db := newDB(t)
	repo := NewUserRepository(db, idgen.New(1))
	_, err := repo.CreateUser(context.Background(), model.User{ID: 1, Email: "a@x", Credits: credits, CreditsRenewAt: renewAt})
	require.NoError(t, err)
	return db, repo
}

func credits(t *testing.T, repo UserRepository) int {
	t.Helper()
	u, err := repo.FindUserByID(context.Background(), 1)
	require.NoError(t, err)
	return u.Credits
}

func remaining(t *testing.T, db *gorm.DB) []int {
	t.Helper()
	var gs []model.CreditGrant
	require.NoError(t, db.Order("expires_at, id").Find(&gs).Error)
	out := []int{}
	for _, g := range gs {
		out = append(out, g.Remaining)
	}
	return out
}

func TestUserRepository_ListUserIDs(t *testing.T) {
	ctx := context.Background()
	repo := NewUserRepository(newDB(t), idgen.New(1))
	for _, id := range []int64{3, 1, 2} {
		_, err := repo.CreateUser(ctx, model.User{ID: id, Email: string(rune('a'+id)) + "@x"})
		require.NoError(t, err)
	}
	ids, err := repo.ListUserIDs(ctx, 0, 2)
	require.NoError(t, err)
	assert.Equal(t, []int64{1, 2}, ids)
	ids, err = repo.ListUserIDs(ctx, 2, 2)
	require.NoError(t, err)
	assert.Equal(t, []int64{3}, ids)
}

func TestUserRepository_GrantCredits(t *testing.T) {
	ctx := context.Background()
	db, repo := newGrantRepo(t, 5, _day.Add(_period))
	expires := _day.Add(30 * 24 * time.Hour)

	require.NoError(t, repo.GrantCredits(ctx, 1, 100, expires))
	assert.Equal(t, 105, credits(t, repo))
	var g model.CreditGrant
	require.NoError(t, db.First(&g).Error)
	assert.Equal(t, int64(1), g.UserID)
	assert.Equal(t, 100, g.Credits)
	assert.Equal(t, 100, g.Remaining)
	assert.True(t, expires.Equal(g.ExpiresAt))
	assert.NotZero(t, g.ID)

	as, err := repo.ListCreditActivities(ctx, 1, 0, 1)
	require.NoError(t, err)
	assert.Equal(t, model.CreditOpAdminGrant, as[0].Ops)
	assert.Equal(t, 100, as[0].Change)
	assert.Equal(t, 105, as[0].Current)

	assert.ErrorIs(t, repo.GrantCredits(ctx, 9, 1, expires), ErrNotFound)
}

func TestUserRepository_SpendTakesWhatExpiresFirst(t *testing.T) {
	ctx := context.Background()
	// Monthly credits (10) renew in 10 days; one grant expires in 5 days,
	// another in 20.
	db, repo := newGrantRepo(t, 10, _day.Add(10*24*time.Hour))
	require.NoError(t, repo.GrantCredits(ctx, 1, 20, _day.Add(20*24*time.Hour)))
	require.NoError(t, repo.GrantCredits(ctx, 1, 4, _day.Add(5*24*time.Hour)))
	assert.Equal(t, 34, credits(t, repo))

	require.NoError(t, repo.SpendCredits(ctx, 1, 3))
	assert.Equal(t, []int{1, 20}, remaining(t, db), "the soonest grant first")

	require.NoError(t, repo.SpendCredits(ctx, 1, 5))
	assert.Equal(t, []int{0, 20}, remaining(t, db), "then the monthly credits")
	assert.Equal(t, 26, credits(t, repo))

	require.NoError(t, repo.SpendCredits(ctx, 1, 12))
	assert.Equal(t, []int{0, 14}, remaining(t, db), "then the later grant")
	assert.Equal(t, 14, credits(t, repo))

	assert.ErrorIs(t, repo.SpendCredits(ctx, 1, 15), ErrInsufficientCredits)
	require.NoError(t, repo.SpendCredits(ctx, 1, 14))
	assert.Equal(t, []int{0, 0}, remaining(t, db))
	assert.Equal(t, 0, credits(t, repo))
}

func TestUserRepository_RenewalKeepsLiveGrants(t *testing.T) {
	ctx := context.Background()
	db, repo := newGrantRepo(t, 10, _day)
	require.NoError(t, repo.GrantCredits(ctx, 1, 50, _day.Add(20*24*time.Hour)))
	require.NoError(t, repo.SpendCredits(ctx, 1, 12)) // 10 monthly, 2 of the grant
	assert.Equal(t, []int{48}, remaining(t, db))

	require.NoError(t, repo.RenewUserCredits(ctx, 1, 250, _day.Add(_period)))
	assert.Equal(t, 298, credits(t, repo), "the monthly part resets, the grant stays")
	as, err := repo.ListCreditActivities(ctx, 1, 0, 1)
	require.NoError(t, err)
	assert.Equal(t, model.CreditOpRenewal, as[0].Ops)
	assert.Equal(t, 250, as[0].Change)
	assert.Equal(t, 298, as[0].Current)
}

func TestUserRepository_ExpireCreditGrants(t *testing.T) {
	ctx := context.Background()
	db, repo := newGrantRepo(t, 10, _day.Add(_period))
	require.NoError(t, repo.GrantCredits(ctx, 1, 30, _day))
	require.NoError(t, repo.GrantCredits(ctx, 1, 7, _day.Add(-24*time.Hour)))
	require.NoError(t, repo.GrantCredits(ctx, 1, 5, _day.Add(time.Hour)))
	require.NoError(t, repo.SpendCredits(ctx, 1, 9)) // the oldest grant, then 2 of the next

	expired, err := repo.ListExpiredCreditGrants(ctx, _day, 10)
	require.NoError(t, err)
	require.Len(t, expired, 1, "spent and future grants are left out")
	assert.Equal(t, 28, expired[0].Remaining)

	require.NoError(t, repo.ExpireCreditGrant(ctx, expired[0]))
	assert.Equal(t, 15, credits(t, repo), "10 monthly + 5 of the live grant")
	assert.Equal(t, []int{0, 0, 5}, remaining(t, db))
	as, err := repo.ListCreditActivities(ctx, 1, 0, 1)
	require.NoError(t, err)
	assert.Equal(t, model.CreditOpExpiry, as[0].Ops)
	assert.Equal(t, -28, as[0].Change)
	assert.Equal(t, 15, as[0].Current)

	require.NoError(t, repo.ExpireCreditGrant(ctx, expired[0]), "expiring twice is a no-op")
	assert.Equal(t, 15, credits(t, repo))

	page, err := repo.ListExpiredCreditGrants(ctx, _day.Add(time.Hour), 10)
	require.NoError(t, err)
	assert.Len(t, page, 1)

	assert.ErrorIs(t, repo.ExpireCreditGrant(ctx, model.CreditGrant{ID: 99, UserID: 1}), gorm.ErrRecordNotFound)
}

func TestUserRepository_CreditGrantErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("closed db", func(t *testing.T) {
		db := newDB(t)
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
		repo := NewUserRepository(db, idgen.New(1))
		_, err = repo.ListUserIDs(ctx, 0, 1)
		assert.Error(t, err)
		_, err = repo.ListExpiredCreditGrants(ctx, _day, 1)
		assert.Error(t, err)
		assert.Error(t, repo.GrantCredits(ctx, 1, 1, _day))
	})

	t.Run("grants table missing", func(t *testing.T) {
		db, repo := newGrantRepo(t, 10, _day)
		require.NoError(t, db.Migrator().DropTable(&model.CreditGrant{}))
		assert.Error(t, repo.GrantCredits(ctx, 1, 1, _day))
		assert.Error(t, repo.SpendCredits(ctx, 1, 1))
		assert.Error(t, repo.RenewUserCredits(ctx, 1, 250, _day))
		assert.Equal(t, 10, credits(t, repo), "rolled back")
	})

	t.Run("activities table missing", func(t *testing.T) {
		db, repo := newGrantRepo(t, 10, _day.Add(_period))
		require.NoError(t, repo.GrantCredits(ctx, 1, 5, _day))
		require.NoError(t, db.Migrator().DropTable(&model.CreditActivity{}))
		assert.Error(t, repo.GrantCredits(ctx, 1, 1, _day))
		g, err := repo.ListExpiredCreditGrants(ctx, _day, 1)
		require.NoError(t, err)
		assert.Error(t, repo.ExpireCreditGrant(ctx, g[0]))
		assert.Equal(t, 15, credits(t, repo), "rolled back")
	})

	t.Run("reading the balance fails", func(t *testing.T) {
		db, repo := newGrantRepo(t, 10, _day)
		require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:fail-read", func(tx *gorm.DB) {
			if tx.Statement.Table == "users" && len(tx.Statement.Selects) == 2 {
				_ = tx.AddError(errors.New("read failed"))
			}
		}))
		err := repo.SpendCredits(ctx, 1, 1)
		assert.Error(t, err)
		assert.NotErrorIs(t, err, ErrInsufficientCredits)
	})

	t.Run("updating a grant fails", func(t *testing.T) {
		db, repo := newGrantRepo(t, 0, _day)
		require.NoError(t, repo.GrantCredits(ctx, 1, 5, _day))
		failUpdates(t, db, "remaining")
		assert.Error(t, repo.SpendCredits(ctx, 1, 1))
		assert.Error(t, repo.ExpireCreditGrant(ctx, model.CreditGrant{ID: firstGrantID(t, db), UserID: 1}))
		assert.Equal(t, 5, credits(t, repo))
	})

	t.Run("taking credits fails", func(t *testing.T) {
		db, repo := newGrantRepo(t, 10, _day)
		require.NoError(t, repo.GrantCredits(ctx, 1, 5, _day))
		failCreditsExpr(t, db, "-")
		assert.Error(t, repo.SpendCredits(ctx, 1, 1))
		assert.Error(t, repo.ExpireCreditGrant(ctx, model.CreditGrant{ID: firstGrantID(t, db), UserID: 1}))
		assert.Equal(t, 15, credits(t, repo))
	})

	t.Run("adding credits fails", func(t *testing.T) {
		db, repo := newGrantRepo(t, 10, _day)
		failCreditsExpr(t, db, "+")
		assert.Error(t, repo.GrantCredits(ctx, 1, 5, _day))
		assert.Equal(t, 10, credits(t, repo))
	})

	t.Run("locking fails", func(t *testing.T) {
		db, repo := newGrantRepo(t, 10, _day)
		require.NoError(t, repo.GrantCredits(ctx, 1, 5, _day))
		id := firstGrantID(t, db)
		require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:fail-lock", func(tx *gorm.DB) {
			if m, ok := tx.Statement.Dest.(map[string]any); ok {
				if e, ok := m["credits"].(clause.Expr); ok && e.SQL == "credits" {
					_ = tx.AddError(errors.New("lock failed"))
				}
			}
		}))
		assert.Error(t, repo.ExpireCreditGrant(ctx, model.CreditGrant{ID: id, UserID: 1}))
	})
}

func firstGrantID(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var g model.CreditGrant
	require.NoError(t, db.First(&g).Error)
	return g.ID
}
