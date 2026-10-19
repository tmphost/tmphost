// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package repository is the data access layer.
package repository

//go:generate mockgen -source=user.go -destination=mocks/user.go -package=mocks

import (
	"context"
	"errors"
	"sort"
	"time"

	"gorm.io/gorm"

	"github.com/tmphost/tmphost/backend/internal/data/model"
	"github.com/tmphost/tmphost/backend/internal/service/idgen"
)

var (
	// ErrNotFound is returned when a record does not exist.
	ErrNotFound = errors.New("not found")
	// ErrInsufficientCredits is returned when a user cannot cover a spend.
	ErrInsufficientCredits = errors.New("insufficient credits")
)

type (
	// UserRepository persists users.
	UserRepository interface {
		FindUserByID(ctx context.Context, id int64) (model.User, error)
		FindUserByEmail(ctx context.Context, email string) (model.User, error)
		CreateUser(ctx context.Context, u model.User) (model.User, error)
		UpdateUser(ctx context.Context, u model.User) (model.User, error)
		// ListUsersDueForCreditRenewal returns up to limit users whose
		// credit renewal time is at or before now, oldest first.
		ListUsersDueForCreditRenewal(ctx context.Context, now time.Time, limit int) ([]model.User, error)
		// ListUserIDs returns up to limit user IDs above after, in order.
		ListUserIDs(ctx context.Context, after int64, limit int) ([]int64, error)
		// RenewUserCredits resets the monthly part of a user's balance to
		// credits and sets the next renewal time. What is left of live
		// admin grants stays on top.
		RenewUserCredits(ctx context.Context, id int64, credits int, renewAt time.Time) error
		// SpendCredits takes n credits from a user in one transaction, or
		// returns ErrInsufficientCredits and takes none. The credits that
		// expire first go first: the monthly part expires at renewal, an
		// admin grant at its ExpiresAt.
		SpendCredits(ctx context.Context, id int64, n int) error
		// RefundCredits gives n spent credits back to the monthly part.
		RefundCredits(ctx context.Context, id int64, n int) error
		// GrantCredits adds an admin grant of n credits to a user, which
		// expires at expiresAt. It returns ErrNotFound for an unknown user.
		GrantCredits(ctx context.Context, id int64, n int, expiresAt time.Time) error
		// ListExpiredCreditGrants returns up to limit admin grants that
		// expired at or before now and still have credits left.
		ListExpiredCreditGrants(ctx context.Context, now time.Time, limit int) ([]model.CreditGrant, error)
		// ExpireCreditGrant takes what is left of an admin grant (one
		// ListExpiredCreditGrants returned) away from its user.
		ExpireCreditGrant(ctx context.Context, g model.CreditGrant) error
		// ListCreditActivities returns up to limit of a user's credit
		// activities, newest first, with IDs below before (0: from the
		// newest).
		ListCreditActivities(ctx context.Context, userID, before int64, limit int) ([]model.CreditActivity, error)
	}

	userRepository struct {
		db *gorm.DB
		// ids issues credit activity IDs.
		ids idgen.Service
	}
)

// NewUserRepository returns a GORM-backed user repository. Credit
// activities take their IDs from ids, so they are monoflake IDs ordered
// by time.
func NewUserRepository(db *gorm.DB, ids idgen.Service) UserRepository {
	return &userRepository{db: db, ids: ids}
}

func (r *userRepository) FindUserByID(ctx context.Context, id int64) (model.User, error) {
	return r.first(ctx, "id = ?", id)
}

func (r *userRepository) FindUserByEmail(ctx context.Context, email string) (model.User, error) {
	return r.first(ctx, "email = ?", email)
}

// CreateUser stores a new user and the grant it starts with.
func (r *userRepository) CreateUser(ctx context.Context, u model.User) (model.User, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&u).Error; err != nil {
			return err
		}
		return r.record(tx, u.ID, model.CreditOpGrant, u.Credits)
	})
	if err != nil {
		return model.User{}, err
	}
	return u, nil
}

func (r *userRepository) UpdateUser(ctx context.Context, u model.User) (model.User, error) {
	if err := r.db.WithContext(ctx).Save(&u).Error; err != nil {
		return model.User{}, err
	}
	return u, nil
}

func (r *userRepository) ListUsersDueForCreditRenewal(ctx context.Context, now time.Time, limit int) ([]model.User, error) {
	var users []model.User
	err := r.db.WithContext(ctx).
		Where("credits_renew_at <= ?", now).
		Order("credits_renew_at, id").
		Limit(limit).
		Find(&users).Error
	if err != nil {
		return nil, err
	}
	return users, nil
}

func (r *userRepository) ListUserIDs(ctx context.Context, after int64, limit int) ([]int64, error) {
	var ids []int64
	err := r.db.WithContext(ctx).Model(&model.User{}).
		Where("id > ?", after).
		Order("id").
		Limit(limit).
		Pluck("id", &ids).Error
	if err != nil {
		return nil, err
	}
	return ids, nil
}

func (r *userRepository) RenewUserCredits(ctx context.Context, id int64, credits int, renewAt time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lock(tx, id); err != nil {
			return err
		}
		old, err := balance(tx, id)
		if err != nil {
			return err
		}
		grants, err := liveGrants(tx, id)
		if err != nil {
			return err
		}
		credits += granted(grants)
		err = tx.Model(&model.User{}).Where("id = ?", id).
			Updates(map[string]any{"credits": credits, "credits_renew_at": renewAt}).Error
		if err != nil {
			return err
		}
		return r.record(tx, id, model.CreditOpRenewal, credits-old)
	})
}

func (r *userRepository) SpendCredits(ctx context.Context, id int64, n int) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lock(tx, id); err != nil {
			return err
		}
		var u model.User
		err := tx.Select("credits", "credits_renew_at").Where("id = ?", id).First(&u).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		// An unknown user has no credits.
		if err != nil || u.Credits < n {
			return ErrInsufficientCredits
		}
		grants, err := liveGrants(tx, id)
		if err != nil {
			return err
		}
		for _, g := range spendFromGrants(grants, u.Credits-granted(grants), u.CreditsRenewAt, n) {
			if err := tx.Model(&model.CreditGrant{}).Where("id = ?", g.ID).Update("remaining", g.Remaining).Error; err != nil {
				return err
			}
		}
		err = tx.Model(&model.User{}).Where("id = ?", id).
			Update("credits", gorm.Expr("credits - ?", n)).Error
		if err != nil {
			return err
		}
		return r.record(tx, id, model.CreditOpSpend, -n)
	})
}

func (r *userRepository) RefundCredits(ctx context.Context, id int64, n int) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Model(&model.User{}).Where("id = ?", id).
			Update("credits", gorm.Expr("credits + ?", n)).Error
		if err != nil {
			return err
		}
		return r.record(tx, id, model.CreditOpRefund, n)
	})
}

func (r *userRepository) ListCreditActivities(ctx context.Context, userID, before int64, limit int) ([]model.CreditActivity, error) {
	q := r.db.WithContext(ctx).Where("user_id = ?", userID)
	if before > 0 {
		q = q.Where("id < ?", before)
	}
	var as []model.CreditActivity
	if err := q.Order("id DESC").Limit(limit).Find(&as).Error; err != nil {
		return nil, err
	}
	return as, nil
}

func (r *userRepository) GrantCredits(ctx context.Context, id int64, n int, expiresAt time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&model.User{}).Where("id = ?", id).
			Update("credits", gorm.Expr("credits + ?", n))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		g := model.CreditGrant{ID: r.ids.NextID(), UserID: id, ExpiresAt: expiresAt, Credits: n, Remaining: n}
		if err := tx.Create(&g).Error; err != nil {
			return err
		}
		return r.record(tx, id, model.CreditOpAdminGrant, n)
	})
}

func (r *userRepository) ListExpiredCreditGrants(ctx context.Context, now time.Time, limit int) ([]model.CreditGrant, error) {
	var gs []model.CreditGrant
	err := r.db.WithContext(ctx).
		Where("expires_at <= ? AND remaining > 0", now).
		Order("expires_at, id").
		Limit(limit).
		Find(&gs).Error
	if err != nil {
		return nil, err
	}
	return gs, nil
}

func (r *userRepository) ExpireCreditGrant(ctx context.Context, g model.CreditGrant) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Lock the user first, so a spend can't take from the grant while
		// it expires; then read what is left of it.
		if err := lock(tx, g.UserID); err != nil {
			return err
		}
		if err := tx.Where("id = ?", g.ID).First(&g).Error; err != nil {
			return err
		}
		if g.Remaining == 0 {
			return nil
		}
		if err := tx.Model(&model.CreditGrant{}).Where("id = ?", g.ID).Update("remaining", 0).Error; err != nil {
			return err
		}
		err := tx.Model(&model.User{}).Where("id = ?", g.UserID).
			Update("credits", gorm.Expr("credits - ?", g.Remaining)).Error
		if err != nil {
			return err
		}
		return r.record(tx, g.UserID, model.CreditOpExpiry, -g.Remaining)
	})
}

// lock takes a user's row lock inside tx with a no-op write, so no other
// balance change lands between reading the balance and changing it. GORM's
// FOR UPDATE isn't portable to SQLite.
func lock(tx *gorm.DB, id int64) error {
	return tx.Model(&model.User{}).Where("id = ?", id).Update("credits", gorm.Expr("credits")).Error
}

// liveGrants returns a user's admin grants that still have credits left,
// inside tx.
func liveGrants(tx *gorm.DB, id int64) ([]model.CreditGrant, error) {
	var gs []model.CreditGrant
	if err := tx.Where("user_id = ? AND remaining > 0", id).Order("expires_at, id").Find(&gs).Error; err != nil {
		return nil, err
	}
	return gs, nil
}

// granted sums what is left of grants.
func granted(gs []model.CreditGrant) int {
	n := 0
	for _, g := range gs {
		n += g.Remaining
	}
	return n
}

// spendFromGrants takes n credits, the ones that expire first first: the
// monthly part (expiring at renewAt) and each grant (at its ExpiresAt). It
// returns the grants it took from, with their new Remaining.
func spendFromGrants(gs []model.CreditGrant, monthly int, renewAt time.Time, n int) []model.CreditGrant {
	type bucket struct {
		grant     int // index into gs; -1 is the monthly part
		expiresAt time.Time
		left      int
	}
	bs := []bucket{{grant: -1, expiresAt: renewAt, left: max(monthly, 0)}}
	for i, g := range gs {
		bs = append(bs, bucket{grant: i, expiresAt: g.ExpiresAt, left: g.Remaining})
	}
	sort.SliceStable(bs, func(i, j int) bool { return bs[i].expiresAt.Before(bs[j].expiresAt) })
	var changed []model.CreditGrant
	for _, b := range bs {
		if n == 0 {
			break
		}
		take := min(n, b.left)
		n -= take
		if b.grant >= 0 && take > 0 {
			g := gs[b.grant]
			g.Remaining -= take
			changed = append(changed, g)
		}
	}
	return changed
}

// balance reads a user's credit balance inside tx.
func balance(tx *gorm.DB, id int64) (int, error) {
	var u model.User
	if err := tx.Select("credits").Where("id = ?", id).First(&u).Error; err != nil {
		return 0, err
	}
	return u.Credits, nil
}

// record adds a credit activity for a change just made inside tx, with the
// balance it left.
func (r *userRepository) record(tx *gorm.DB, id int64, ops uint8, change int) error {
	current, err := balance(tx, id)
	if err != nil {
		return err
	}
	return tx.Create(&model.CreditActivity{ID: r.ids.NextID(), UserID: id, Ops: ops, Change: change, Current: current}).Error
}

func (r *userRepository) first(ctx context.Context, query string, arg any) (model.User, error) {
	var u model.User
	err := r.db.WithContext(ctx).Where(query, arg).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.User{}, ErrNotFound
	}
	if err != nil {
		return model.User{}, err
	}
	return u, nil
}
