// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/tmphost/tmphost/backend/internal/data/model"
	"github.com/tmphost/tmphost/backend/internal/service/idgen"
	idgenmocks "github.com/tmphost/tmphost/backend/internal/service/idgen/mocks"
)

func newDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Folder{}, &model.CreditActivity{}, &model.CreditGrant{}, &model.File{}))
	return db
}

func TestUserRepository_CRUD(t *testing.T) {
	ctx := context.Background()
	repo := NewUserRepository(newDB(t), idgen.New(1))

	_, err := repo.FindUserByEmail(ctx, "a@b.c")
	assert.ErrorIs(t, err, ErrNotFound)

	renew := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	created, err := repo.CreateUser(ctx, model.User{ID: 1, Email: "a@b.c", Name: "A", Credits: 250, CreditsRenewAt: renew})
	require.NoError(t, err)
	assert.Equal(t, int64(1), created.ID)

	_, err = repo.CreateUser(ctx, model.User{ID: 2, Email: "a@b.c"})
	assert.Error(t, err, "email is unique")

	byEmail, err := repo.FindUserByEmail(ctx, "a@b.c")
	require.NoError(t, err)
	assert.Equal(t, "A", byEmail.Name)
	assert.True(t, renew.Equal(byEmail.CreditsRenewAt))

	byEmail.Credits = 10
	updated, err := repo.UpdateUser(ctx, byEmail)
	require.NoError(t, err)
	assert.Equal(t, 10, updated.Credits)

	byID, err := repo.FindUserByID(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 10, byID.Credits)

	_, err = repo.FindUserByID(ctx, 99)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestUserRepository_DBErrors(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	repo := NewUserRepository(db, idgen.New(1))

	_, err = repo.FindUserByID(ctx, 1)
	assert.Error(t, err)
	assert.NotErrorIs(t, err, ErrNotFound)

	_, err = repo.UpdateUser(ctx, model.User{ID: 1, Email: "x"})
	assert.Error(t, err)

	_, err = repo.ListUsersDueForCreditRenewal(ctx, time.Now(), 10)
	assert.Error(t, err)

	assert.Error(t, repo.RenewUserCredits(ctx, 1, 250, time.Now()))
	assert.Error(t, repo.SpendCredits(ctx, 1, 1, ""))
	assert.NotErrorIs(t, repo.SpendCredits(ctx, 1, 1, ""), ErrInsufficientCredits)
	assert.Error(t, repo.RefundCredits(ctx, 1, 1, ""))
}

func TestUserRepository_SpendAndRefundCredits(t *testing.T) {
	ctx := context.Background()
	repo := NewUserRepository(newDB(t), idgen.New(1))
	_, err := repo.CreateUser(ctx, model.User{ID: 1, Email: "a@x", Credits: 5})
	require.NoError(t, err)

	require.NoError(t, repo.SpendCredits(ctx, 1, 3, ""))
	assert.ErrorIs(t, repo.SpendCredits(ctx, 1, 3, ""), ErrInsufficientCredits, "only 2 left")
	assert.ErrorIs(t, repo.SpendCredits(ctx, 99, 1, ""), ErrInsufficientCredits, "unknown user")
	require.NoError(t, repo.SpendCredits(ctx, 1, 2, "Claude Code"))
	require.NoError(t, repo.RefundCredits(ctx, 1, 4, "tmphost CLI"))

	u, err := repo.FindUserByID(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 4, u.Credits)

	as, err := repo.ListCreditActivities(ctx, 1, 0, 10)
	require.NoError(t, err)
	agents := []string{}
	for _, a := range as {
		agents = append(agents, a.Agent)
	}
	assert.Equal(t, []string{"tmphost CLI", "Claude Code", "", ""}, agents, "the system's sign-up grant names no agent")
}

func TestUserRepository_CreditRenewal(t *testing.T) {
	ctx := context.Background()
	repo := NewUserRepository(newDB(t), idgen.New(1))
	now := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)

	for i, renewAt := range []time.Time{now.Add(time.Hour), now, now.Add(-48 * time.Hour), now.Add(-24 * time.Hour)} {
		_, err := repo.CreateUser(ctx, model.User{ID: int64(i + 1), Email: string(rune('a'+i)) + "@x", Name: "keep", Credits: 1, CreditsRenewAt: renewAt})
		require.NoError(t, err)
	}

	due, err := repo.ListUsersDueForCreditRenewal(ctx, now, 10)
	require.NoError(t, err)
	ids := []int64{}
	for _, u := range due {
		ids = append(ids, u.ID)
	}
	assert.Equal(t, []int64{3, 4, 2}, ids, "oldest first, future excluded")

	page, err := repo.ListUsersDueForCreditRenewal(ctx, now, 1)
	require.NoError(t, err)
	assert.Len(t, page, 1)

	next := now.Add(30 * 24 * time.Hour)
	require.NoError(t, repo.RenewUserCredits(ctx, 3, 250, next))
	u, err := repo.FindUserByID(ctx, 3)
	require.NoError(t, err)
	assert.Equal(t, 250, u.Credits)
	assert.True(t, next.Equal(u.CreditsRenewAt))
	assert.Equal(t, "keep", u.Name, "other columns untouched")
}

func TestUserRepository_CreditActivities(t *testing.T) {
	ctx := context.Background()
	repo := NewUserRepository(newDB(t), idgen.New(1))
	_, err := repo.CreateUser(ctx, model.User{ID: 1, Email: "a@x", Credits: 5})
	require.NoError(t, err)
	_, err = repo.CreateUser(ctx, model.User{ID: 2, Email: "b@x", Credits: 9})
	require.NoError(t, err)

	require.NoError(t, repo.SpendCredits(ctx, 1, 3, ""))
	assert.ErrorIs(t, repo.SpendCredits(ctx, 1, 3, ""), ErrInsufficientCredits)
	require.NoError(t, repo.RefundCredits(ctx, 1, 4, ""))
	require.NoError(t, repo.RenewUserCredits(ctx, 1, 250, time.Now()))

	type row struct {
		ops             uint8
		change, current int
	}
	all, err := repo.ListCreditActivities(ctx, 1, 0, 10)
	require.NoError(t, err)
	got := []row{}
	for _, a := range all {
		assert.Equal(t, int64(1), a.UserID)
		assert.False(t, a.CreatedAt.IsZero())
		got = append(got, row{a.Ops, a.Change, a.Current})
	}
	assert.Equal(t, []row{
		{model.CreditOpRenewal, 244, 250},
		{model.CreditOpRefund, 4, 6},
		{model.CreditOpSpend, -3, 2},
		{model.CreditOpGrant, 5, 5},
	}, got, "newest first; the failed spend left no row; user 2's rows are not listed")

	page, err := repo.ListCreditActivities(ctx, 1, all[1].ID, 2)
	require.NoError(t, err)
	require.Len(t, page, 2)
	assert.Equal(t, all[2].ID, page[0].ID)
	assert.Equal(t, all[3].ID, page[1].ID)
}

func TestUserRepository_CreditActivityIDsComeFromIDGen(t *testing.T) {
	ctx := context.Background()
	ids := idgenmocks.NewMockService(gomock.NewController(t))
	gomock.InOrder(
		ids.EXPECT().NextID().Return(int64(7_000_000_000_000_000_001)),
		ids.EXPECT().NextID().Return(int64(7_000_000_000_000_000_002)),
	)
	repo := NewUserRepository(newDB(t), ids)
	_, err := repo.CreateUser(ctx, model.User{ID: 1, Email: "a@x", Credits: 5})
	require.NoError(t, err)
	require.NoError(t, repo.SpendCredits(ctx, 1, 1, ""))

	as, err := repo.ListCreditActivities(ctx, 1, 0, 10)
	require.NoError(t, err)
	require.Len(t, as, 2)
	assert.Equal(t, int64(7_000_000_000_000_000_002), as[0].ID)
	assert.Equal(t, int64(7_000_000_000_000_000_001), as[1].ID)
}

// failUpdates makes every update of db whose values include column fail.
func failUpdates(t *testing.T, db *gorm.DB, column string) {
	t.Helper()
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:fail", func(tx *gorm.DB) {
		if m, ok := tx.Statement.Dest.(map[string]any); ok {
			if _, ok := m[column]; ok {
				_ = tx.AddError(errors.New("update failed"))
			}
		}
	}))
}

func TestUserRepository_CreditActivityErrorsRollBack(t *testing.T) {
	ctx := context.Background()

	t.Run("activity insert fails", func(t *testing.T) {
		db := newDB(t)
		repo := NewUserRepository(db, idgen.New(1))
		require.NoError(t, db.Create(&model.User{ID: 1, Email: "a@x", Credits: 5}).Error)
		require.NoError(t, db.Migrator().DropTable(&model.CreditActivity{}))

		_, err := repo.CreateUser(ctx, model.User{ID: 2, Email: "b@x", Credits: 5})
		assert.Error(t, err)
		_, err = repo.FindUserByID(ctx, 2)
		assert.ErrorIs(t, err, ErrNotFound, "user creation rolled back")

		assert.Error(t, repo.SpendCredits(ctx, 1, 1, ""))
		assert.Error(t, repo.RefundCredits(ctx, 1, 1, ""))
		assert.Error(t, repo.RenewUserCredits(ctx, 1, 250, time.Now()))
		u, err := repo.FindUserByID(ctx, 1)
		require.NoError(t, err)
		assert.Equal(t, 5, u.Credits, "balance changes rolled back")
	})

	t.Run("unknown user", func(t *testing.T) {
		repo := NewUserRepository(newDB(t), idgen.New(1))
		assert.ErrorIs(t, repo.RefundCredits(ctx, 9, 1, ""), gorm.ErrRecordNotFound)
		assert.ErrorIs(t, repo.RenewUserCredits(ctx, 9, 250, time.Now()), gorm.ErrRecordNotFound)
	})

	t.Run("renewal update fails", func(t *testing.T) {
		db := newDB(t)
		repo := NewUserRepository(db, idgen.New(1))
		_, err := repo.CreateUser(ctx, model.User{ID: 1, Email: "a@x", Credits: 5})
		require.NoError(t, err)
		failUpdates(t, db, "credits_renew_at")
		assert.Error(t, repo.RenewUserCredits(ctx, 1, 250, time.Now()))
		as, err := repo.ListCreditActivities(ctx, 1, 0, 10)
		require.NoError(t, err)
		assert.Len(t, as, 1, "only the grant")
	})

	t.Run("balance update fails", func(t *testing.T) {
		db := newDB(t)
		repo := NewUserRepository(db, idgen.New(1))
		_, err := repo.CreateUser(ctx, model.User{ID: 1, Email: "a@x", Credits: 5})
		require.NoError(t, err)
		failUpdates(t, db, "credits")
		assert.Error(t, repo.SpendCredits(ctx, 1, 1, ""))
		assert.NotErrorIs(t, repo.SpendCredits(ctx, 1, 1, ""), ErrInsufficientCredits)
		assert.Error(t, repo.RefundCredits(ctx, 1, 1, ""))
		assert.Error(t, repo.RenewUserCredits(ctx, 1, 250, time.Now()))
	})

	t.Run("closed database", func(t *testing.T) {
		db := newDB(t)
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
		_, err = NewUserRepository(db, idgen.New(1)).ListCreditActivities(ctx, 1, 0, 10)
		assert.Error(t, err)
	})
}
