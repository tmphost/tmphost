// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package credit implements the operation credit grants: a new account
// starts with the sign-up grant (DefaultSignupGrant unless root changed
// it), and every Period a daily job that runs at 00:00 UTC resets the
// balance to the monthly grant (DefaultMonthlyGrant unless root changed
// it). Root can also grant credits to one user or to everyone; those
// expire after Period, and the same job takes what is left of them away.
// Every change to a balance is kept as a credit activity.
package credit

//go:generate mockgen -source=credit.go -destination=mocks/credit.go -package=mocks

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tmphost/tmphost/backend/internal/data/entity"
	mappermodel "github.com/tmphost/tmphost/backend/internal/mapper/model"
	"github.com/tmphost/tmphost/backend/internal/repository"
)

const (
	// DefaultMonthlyGrant is the operation credit balance each period starts
	// with, unless configured otherwise.
	DefaultMonthlyGrant = 250
	// DefaultSignupGrant is the balance a new account starts with, unless
	// configured otherwise.
	DefaultSignupGrant = 1000
	// Period is how often the grant renews.
	Period = 30 * 24 * time.Hour
	// RenewalSchedule is the cron spec (UTC) of the renewal job.
	RenewalSchedule = "0 0 * * *"

	// DefaultHistoryLimit is the credit history page size unless asked
	// otherwise.
	DefaultHistoryLimit = 50
	// MaxHistoryLimit is the largest credit history page.
	MaxHistoryLimit = 200

	// MaxAdminGrant is the most credits root can grant at once.
	MaxAdminGrant = 1_000_000

	_batchSize = 500
)

var (
	// ErrInvalidGrant is returned for a grant with a bad amount or target.
	ErrInvalidGrant = errors.New("invalid grant")
	// ErrUserNotFound is returned when a grant names an unknown user.
	ErrUserNotFound = errors.New("user not found")
)

type (
	// Controller renews credits and reports their history.
	Controller interface {
		// RenewDueCredits expires every admin grant whose time has passed,
		// then resets every account whose renewal time has passed.
		RenewDueCredits(ctx context.Context) (*entity.RenewDueCreditsResponse, error)
		// GrantCredits gives credits that expire after Period to one user
		// or to every user.
		GrantCredits(ctx context.Context, rq *entity.GrantCreditsRequest) (*entity.GrantCreditsResponse, error)
		// ListCreditActivities returns a page of a user's credit history,
		// newest first.
		ListCreditActivities(ctx context.Context, rq *entity.ListCreditActivitiesRequest) (*entity.ListCreditActivitiesResponse, error)
	}

	// Params are the controller's dependencies.
	Params struct {
		Users repository.UserRepository
		// Grant returns the current monthly grant; it can change at runtime.
		Grant     func() int
		Now       func() time.Time
		BatchSize int
	}

	controller struct {
		users     repository.UserRepository
		grant     func() int
		now       func() time.Time
		batchSize int
	}
)

// New returns the credit controller.
func New(p Params) Controller {
	c := &controller{users: p.Users, grant: GrantOrDefault(p.Grant), now: p.Now, batchSize: p.BatchSize}
	if c.now == nil {
		c.now = time.Now
	}
	if c.batchSize <= 0 {
		c.batchSize = _batchSize
	}
	return c
}

// GrantOrDefault returns grant, or a source of DefaultMonthlyGrant when
// grant is nil.
func GrantOrDefault(grant func() int) func() int {
	if grant != nil {
		return grant
	}
	return func() int { return DefaultMonthlyGrant }
}

// SignupGrantOrDefault returns grant, or a source of DefaultSignupGrant
// when grant is nil.
func SignupGrantOrDefault(grant func() int) func() int {
	if grant != nil {
		return grant
	}
	return func() int { return DefaultSignupGrant }
}

// FirstRenewal is when a newly registered account's credits first renew:
// Period after the start (00:00 UTC) of the registration day, so it lines
// up with a run of the daily job.
func FirstRenewal(registeredAt time.Time) time.Time {
	return registeredAt.UTC().Truncate(24 * time.Hour).Add(Period)
}

// NextRenewal moves a passed renewal time forward by whole periods until it
// is after now, keeping the cycle anchored to the registration day.
func NextRenewal(renewAt, now time.Time) time.Time {
	if now.Before(renewAt) {
		return renewAt
	}
	periods := now.Sub(renewAt)/Period + 1
	return renewAt.Add(periods * Period)
}

// GrantExpiry is when an admin grant made at grantedAt expires: Period
// later, at the next 00:00 UTC, so it lines up with a run of the daily job.
func GrantExpiry(grantedAt time.Time) time.Time {
	at := grantedAt.UTC().Add(Period)
	day := at.Truncate(24 * time.Hour)
	if day.Equal(at) {
		return day
	}
	return day.Add(24 * time.Hour)
}

func (c *controller) GrantCredits(ctx context.Context, rq *entity.GrantCreditsRequest) (*entity.GrantCreditsResponse, error) {
	email := strings.TrimSpace(rq.Email)
	if rq.Credits <= 0 || rq.Credits > MaxAdminGrant {
		return nil, fmt.Errorf("%w: credits must be between 1 and %d", ErrInvalidGrant, MaxAdminGrant)
	}
	if rq.All == (email != "") {
		return nil, fmt.Errorf("%w: name one user by email, or grant to all users", ErrInvalidGrant)
	}
	rs := &entity.GrantCreditsResponse{ExpiresAt: GrantExpiry(c.now())}
	if !rq.All {
		u, err := c.users.FindUserByEmail(ctx, email)
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrUserNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("find user: %w", err)
		}
		if err := c.users.GrantCredits(ctx, u.ID, rq.Credits, rs.ExpiresAt); err != nil {
			return nil, fmt.Errorf("grant user %d: %w", u.ID, err)
		}
		rs.Users = 1
		return rs, nil
	}
	var after int64
	for {
		ids, err := c.users.ListUserIDs(ctx, after, c.batchSize)
		if err != nil {
			return rs, fmt.Errorf("list users: %w", err)
		}
		for _, id := range ids {
			// A user deleted since the listing just doesn't get it.
			err := c.users.GrantCredits(ctx, id, rq.Credits, rs.ExpiresAt)
			if errors.Is(err, repository.ErrNotFound) {
				continue
			}
			if err != nil {
				return rs, fmt.Errorf("grant user %d: %w", id, err)
			}
			rs.Users++
		}
		if len(ids) < c.batchSize {
			return rs, nil
		}
		after = ids[len(ids)-1]
	}
}

func (c *controller) RenewDueCredits(ctx context.Context) (*entity.RenewDueCreditsResponse, error) {
	now, grant := c.now(), c.grant()
	expired, err := c.expireGrants(ctx, now)
	if err != nil {
		return &entity.RenewDueCreditsResponse{Expired: expired}, err
	}
	renewed := 0
	for {
		// Each renewed user's renewal time moves past now, so the next page
		// only returns users not yet handled.
		users, err := c.users.ListUsersDueForCreditRenewal(ctx, now, c.batchSize)
		if err != nil {
			return &entity.RenewDueCreditsResponse{Renewed: renewed, Expired: expired}, fmt.Errorf("list due users: %w", err)
		}
		for _, u := range users {
			if err := c.users.RenewUserCredits(ctx, u.ID, grant, NextRenewal(u.CreditsRenewAt, now)); err != nil {
				return &entity.RenewDueCreditsResponse{Renewed: renewed, Expired: expired}, fmt.Errorf("renew user %d: %w", u.ID, err)
			}
			renewed++
		}
		if len(users) < c.batchSize {
			return &entity.RenewDueCreditsResponse{Renewed: renewed, Expired: expired}, nil
		}
	}
}

// expireGrants takes what is left of every admin grant expired by now away,
// and returns how many grants it expired.
func (c *controller) expireGrants(ctx context.Context, now time.Time) (int, error) {
	expired := 0
	for {
		// Each expired grant has nothing left, so the next page only
		// returns grants not yet handled.
		grants, err := c.users.ListExpiredCreditGrants(ctx, now, c.batchSize)
		if err != nil {
			return expired, fmt.Errorf("list expired grants: %w", err)
		}
		for _, g := range grants {
			if err := c.users.ExpireCreditGrant(ctx, g); err != nil {
				return expired, fmt.Errorf("expire grant %d: %w", g.ID, err)
			}
			expired++
		}
		if len(grants) < c.batchSize {
			return expired, nil
		}
	}
}

func (c *controller) ListCreditActivities(ctx context.Context, rq *entity.ListCreditActivitiesRequest) (*entity.ListCreditActivitiesResponse, error) {
	limit := rq.Limit
	if limit <= 0 {
		limit = DefaultHistoryLimit
	}
	limit = min(limit, MaxHistoryLimit)
	// One extra row tells whether another page follows.
	ms, err := c.users.ListCreditActivities(ctx, rq.UserID, rq.Before, limit+1)
	if err != nil {
		return nil, fmt.Errorf("list credit activities: %w", err)
	}
	rs := &entity.ListCreditActivitiesResponse{}
	if len(ms) > limit {
		ms = ms[:limit]
		rs.Next = ms[limit-1].ID
	}
	rs.Activities = mappermodel.FromCreditActivityModelsToCreditActivityEntities(ms)
	return rs, nil
}
