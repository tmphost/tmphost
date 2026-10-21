// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package admin reports on every account for the root-only admin page:
// totals and daily stats, the users with their credits and usage, and
// each user's credit history.
package admin

//go:generate mockgen -source=admin.go -destination=mocks/admin.go -package=mocks

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mustafaturan/monoflake"

	creditctrl "github.com/tmphost/tmphost/backend/internal/controller/credit"
	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/data/model"
	mappermodel "github.com/tmphost/tmphost/backend/internal/mapper/model"
	"github.com/tmphost/tmphost/backend/internal/repository"
)

const (
	// StatsDays is how many UTC days, ending today, the stats and each
	// user's usage cover: one renewal period.
	StatsDays = int(creditctrl.Period / _day)
	// DefaultUsersLimit is the user page size unless asked otherwise.
	DefaultUsersLimit = 50
	// MaxUsersLimit is the largest user page.
	MaxUsersLimit = 200

	_day = 24 * time.Hour
)

// ErrUserNotFound is returned for an unknown user.
var ErrUserNotFound = errors.New("user not found")

type (
	// Controller reports on every account.
	Controller interface {
		// GetStats returns the totals over every account and one entry per
		// day of the last StatsDays days.
		GetStats(ctx context.Context, rq *entity.GetAdminStatsRequest) (*entity.GetAdminStatsResponse, error)
		// ListUsers returns a page of users, newest first, with their usage.
		ListUsers(ctx context.Context, rq *entity.ListAdminUsersRequest) (*entity.ListAdminUsersResponse, error)
		// GetUser returns one user with their usage.
		GetUser(ctx context.Context, rq *entity.GetAdminUserRequest) (*entity.GetAdminUserResponse, error)
		// ListUserCreditActivities returns a page of a user's credit
		// history, newest first.
		ListUserCreditActivities(ctx context.Context, rq *entity.ListCreditActivitiesRequest) (*entity.ListCreditActivitiesResponse, error)
	}

	// Params are the controller's dependencies.
	Params struct {
		Users   repository.UserRepository
		Stats   repository.StatsRepository
		Credits creditctrl.Controller
		// Grant returns the current monthly grant; it can change at runtime.
		Grant func() int
		Now   func() time.Time
	}

	controller struct {
		users   repository.UserRepository
		stats   repository.StatsRepository
		credits creditctrl.Controller
		grant   func() int
		now     func() time.Time
	}
)

// New returns the admin controller.
func New(p Params) Controller {
	c := &controller{users: p.Users, stats: p.Stats, credits: p.Credits, grant: creditctrl.GrantOrDefault(p.Grant), now: p.Now}
	if c.now == nil {
		c.now = time.Now
	}
	return c
}

// since is the start (00:00 UTC) of the first stats day.
func (c *controller) since() time.Time {
	return c.now().UTC().Truncate(_day).Add(-time.Duration(StatsDays-1) * _day)
}

func (c *controller) GetStats(ctx context.Context, _ *entity.GetAdminStatsRequest) (*entity.GetAdminStatsResponse, error) {
	now, since := c.now(), c.since()
	totals, err := c.stats.Totals(ctx, now)
	if err != nil {
		return nil, fmt.Errorf("totals: %w", err)
	}
	used, err := c.stats.DailyCreditUsage(ctx, since)
	if err != nil {
		return nil, fmt.Errorf("daily credit usage: %w", err)
	}
	signups, err := c.stats.DailySignups(ctx, since)
	if err != nil {
		return nil, fmt.Errorf("daily signups: %w", err)
	}
	rs := &entity.GetAdminStatsResponse{Users: totals.Users, Credits: totals.Credits, Folders: totals.Folders, Days: make([]entity.AdminStatsDay, StatsDays)}
	index := make(map[string]int, StatsDays)
	for i := range rs.Days {
		rs.Days[i].Date = since.Add(time.Duration(i) * _day).Format(time.DateOnly)
		index[rs.Days[i].Date] = i
	}
	for _, d := range used {
		if i, ok := index[d.Day]; ok {
			rs.Days[i].Used = d.Count
			rs.Used += d.Count
		}
	}
	for _, d := range signups {
		if i, ok := index[d.Day]; ok {
			rs.Days[i].Signups = d.Count
			rs.Signups += d.Count
		}
	}
	return rs, nil
}

func (c *controller) ListUsers(ctx context.Context, rq *entity.ListAdminUsersRequest) (*entity.ListAdminUsersResponse, error) {
	limit := rq.Limit
	if limit <= 0 {
		limit = DefaultUsersLimit
	}
	limit = min(limit, MaxUsersLimit)
	q := strings.TrimSpace(rq.Query)
	// One extra row tells whether another page follows.
	ms, err := c.stats.SearchUsers(ctx, q, queryID(q), rq.Before, limit+1)
	if err != nil {
		return nil, fmt.Errorf("search users: %w", err)
	}
	rs := &entity.ListAdminUsersResponse{}
	if len(ms) > limit {
		ms = ms[:limit]
		rs.Next = ms[limit-1].ID
	}
	rs.Users, err = c.withUsage(ctx, ms)
	if err != nil {
		return nil, err
	}
	return rs, nil
}

// queryID is the user ID a search names, if it is a base62 ID; else 0.
func queryID(q string) int64 {
	if q == "" || strings.ContainsFunc(q, func(r rune) bool {
		return (r < '0' || r > '9') && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z')
	}) {
		return 0
	}
	return max(monoflake.IDFromBase62(q).Int64(), 0)
}

func (c *controller) GetUser(ctx context.Context, rq *entity.GetAdminUserRequest) (*entity.GetAdminUserResponse, error) {
	m, err := c.findUser(ctx, rq.UserID)
	if err != nil {
		return nil, err
	}
	us, err := c.withUsage(ctx, []model.User{m})
	if err != nil {
		return nil, err
	}
	return &entity.GetAdminUserResponse{User: us[0]}, nil
}

func (c *controller) ListUserCreditActivities(ctx context.Context, rq *entity.ListCreditActivitiesRequest) (*entity.ListCreditActivitiesResponse, error) {
	if _, err := c.findUser(ctx, rq.UserID); err != nil {
		return nil, err
	}
	return c.credits.ListCreditActivities(ctx, rq)
}

func (c *controller) findUser(ctx context.Context, id int64) (model.User, error) {
	m, err := c.users.FindUserByID(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return model.User{}, ErrUserNotFound
	}
	if err != nil {
		return model.User{}, fmt.Errorf("find user: %w", err)
	}
	return m, nil
}

// withUsage maps users to admin users, with what each spent over the stats
// days and how many live folders each has.
func (c *controller) withUsage(ctx context.Context, ms []model.User) ([]entity.AdminUser, error) {
	us := make([]entity.AdminUser, len(ms))
	if len(ms) == 0 {
		return us, nil
	}
	ids := make([]int64, len(ms))
	for i, m := range ms {
		ids[i] = m.ID
	}
	used, err := c.stats.CreditUsageByUser(ctx, ids, c.since())
	if err != nil {
		return nil, fmt.Errorf("credit usage: %w", err)
	}
	folders, err := c.stats.LiveFoldersByUser(ctx, ids, c.now())
	if err != nil {
		return nil, fmt.Errorf("live folders: %w", err)
	}
	grant := c.grant()
	for i, m := range ms {
		us[i] = entity.AdminUser{User: mappermodel.FromUserModelToUserEntity(m, grant), Used: used[m.ID], Folders: folders[m.ID]}
	}
	return us, nil
}
