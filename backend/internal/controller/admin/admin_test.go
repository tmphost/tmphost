// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package admin

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	creditmocks "github.com/tmphost/tmphost/backend/internal/controller/credit/mocks"
	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/data/model"
	"github.com/tmphost/tmphost/backend/internal/repository"
	repomocks "github.com/tmphost/tmphost/backend/internal/repository/mocks"
)

var (
	// _now is 2026-10-21 15:30 UTC, so the stats start 2026-09-22.
	_now    = time.Date(2026, 10, 21, 15, 30, 0, 0, time.UTC)
	_since  = time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	errBoom = errors.New("boom")
)

type deps struct {
	users   *repomocks.MockUserRepository
	stats   *repomocks.MockStatsRepository
	credits *creditmocks.MockController
}

func newController(t *testing.T) (Controller, deps) {
	t.Helper()
	ctrl := gomock.NewController(t)
	d := deps{
		users:   repomocks.NewMockUserRepository(ctrl),
		stats:   repomocks.NewMockStatsRepository(ctrl),
		credits: creditmocks.NewMockController(ctrl),
	}
	c := New(Params{Users: d.users, Stats: d.stats, Credits: d.credits, Grant: func() int { return 250 }, Now: func() time.Time { return _now }})
	return c, d
}

func TestNew_Defaults(t *testing.T) {
	c := New(Params{}).(*controller)
	assert.WithinDuration(t, time.Now(), c.now(), time.Second)
	assert.Equal(t, 250, c.grant())
	assert.Equal(t, 30, StatsDays)
}

func TestGetStats(t *testing.T) {
	ctx := context.Background()
	c, d := newController(t)
	d.stats.EXPECT().Totals(ctx, _now).Return(repository.Totals{Users: 3, Credits: 157, Folders: 2}, nil)
	d.stats.EXPECT().DailyCreditUsage(ctx, _since).Return([]repository.DayCount{
		{Day: "2026-09-21", Count: 99}, // before the range: ignored
		{Day: "2026-09-22", Count: 5},
		{Day: "2026-10-21", Count: 4},
	}, nil)
	d.stats.EXPECT().DailySignups(ctx, _since).Return([]repository.DayCount{{Day: "2026-10-20", Count: 2}, {Day: "2026-10-22", Count: 1}}, nil)

	rs, err := c.GetStats(ctx, &entity.GetAdminStatsRequest{})
	require.NoError(t, err)
	assert.Equal(t, int64(3), rs.Users)
	assert.Equal(t, int64(157), rs.Credits)
	assert.Equal(t, int64(2), rs.Folders)
	assert.Equal(t, int64(9), rs.Used)
	assert.Equal(t, int64(2), rs.Signups)
	require.Len(t, rs.Days, StatsDays)
	assert.Equal(t, entity.AdminStatsDay{Date: "2026-09-22", Used: 5}, rs.Days[0])
	assert.Equal(t, entity.AdminStatsDay{Date: "2026-10-20", Signups: 2}, rs.Days[28])
	assert.Equal(t, entity.AdminStatsDay{Date: "2026-10-21", Used: 4}, rs.Days[29])
}

func TestGetStats_Errors(t *testing.T) {
	ctx := context.Background()
	t.Run("totals", func(t *testing.T) {
		c, d := newController(t)
		d.stats.EXPECT().Totals(ctx, _now).Return(repository.Totals{}, errBoom)
		_, err := c.GetStats(ctx, &entity.GetAdminStatsRequest{})
		assert.ErrorIs(t, err, errBoom)
	})
	t.Run("usage", func(t *testing.T) {
		c, d := newController(t)
		d.stats.EXPECT().Totals(ctx, _now).Return(repository.Totals{}, nil)
		d.stats.EXPECT().DailyCreditUsage(ctx, _since).Return(nil, errBoom)
		_, err := c.GetStats(ctx, &entity.GetAdminStatsRequest{})
		assert.ErrorIs(t, err, errBoom)
	})
	t.Run("signups", func(t *testing.T) {
		c, d := newController(t)
		d.stats.EXPECT().Totals(ctx, _now).Return(repository.Totals{}, nil)
		d.stats.EXPECT().DailyCreditUsage(ctx, _since).Return(nil, nil)
		d.stats.EXPECT().DailySignups(ctx, _since).Return(nil, errBoom)
		_, err := c.GetStats(ctx, &entity.GetAdminStatsRequest{})
		assert.ErrorIs(t, err, errBoom)
	})
}

func TestListUsers(t *testing.T) {
	ctx := context.Background()
	c, d := newController(t)
	users := []model.User{{ID: 3, Email: "c@x", Credits: 12000}, {ID: 2, Email: "b@x", Credits: 10}, {ID: 1}}
	d.stats.EXPECT().SearchUsers(ctx, "", int64(0), int64(9), 3).Return(users, nil)
	d.stats.EXPECT().CreditUsageByUser(ctx, []int64{3, 2}, _since).Return(map[int64]int{3: 7}, nil)
	d.stats.EXPECT().LiveFoldersByUser(ctx, []int64{3, 2}, _now).Return(map[int64]int{2: 4}, nil)

	rs, err := c.ListUsers(ctx, &entity.ListAdminUsersRequest{Before: 9, Limit: 2})
	require.NoError(t, err)
	assert.Equal(t, int64(2), rs.Next)
	require.Len(t, rs.Users, 2)
	assert.Equal(t, int64(3), rs.Users[0].ID)
	assert.Equal(t, 7, rs.Users[0].Used)
	assert.Equal(t, 0, rs.Users[0].Folders)
	assert.Equal(t, 20000, rs.Users[0].CreditsLimit, "a balance above 10k raises the limit by 10k")
	assert.Equal(t, 10000, rs.Users[1].CreditsLimit)
	assert.Equal(t, 250, rs.Users[1].MonthlyGrant)
	assert.Equal(t, 4, rs.Users[1].Folders)
}

func TestListUsers_Query(t *testing.T) {
	ctx := context.Background()
	id := monoflake.ID(4242)
	t.Run("ID", func(t *testing.T) {
		c, d := newController(t)
		d.stats.EXPECT().SearchUsers(ctx, id.String(), int64(4242), int64(0), DefaultUsersLimit+1).Return(nil, nil)
		rs, err := c.ListUsers(ctx, &entity.ListAdminUsersRequest{Query: "  " + id.String() + " "})
		require.NoError(t, err)
		assert.Empty(t, rs.Users)
		assert.Zero(t, rs.Next)
	})
	t.Run("email", func(t *testing.T) {
		c, d := newController(t)
		d.stats.EXPECT().SearchUsers(ctx, "a@b.c", int64(0), int64(0), MaxUsersLimit+1).Return(nil, nil)
		_, err := c.ListUsers(ctx, &entity.ListAdminUsersRequest{Query: "a@b.c", Limit: 10_000})
		require.NoError(t, err)
	})
}

func TestQueryID(t *testing.T) {
	assert.Zero(t, queryID(""))
	assert.Zero(t, queryID("a@b"))
	assert.Equal(t, int64(4242), queryID(monoflake.ID(4242).String()))
	assert.Zero(t, queryID("zzzzzzzzzzzzzzzzzzzzzzzz"), "too large for an ID")
}

func TestListUsers_Errors(t *testing.T) {
	ctx := context.Background()
	users := []model.User{{ID: 1}}
	t.Run("search", func(t *testing.T) {
		c, d := newController(t)
		d.stats.EXPECT().SearchUsers(ctx, "", int64(0), int64(0), DefaultUsersLimit+1).Return(nil, errBoom)
		_, err := c.ListUsers(ctx, &entity.ListAdminUsersRequest{})
		assert.ErrorIs(t, err, errBoom)
	})
	t.Run("usage", func(t *testing.T) {
		c, d := newController(t)
		d.stats.EXPECT().SearchUsers(ctx, "", int64(0), int64(0), DefaultUsersLimit+1).Return(users, nil)
		d.stats.EXPECT().CreditUsageByUser(ctx, []int64{1}, _since).Return(nil, errBoom)
		_, err := c.ListUsers(ctx, &entity.ListAdminUsersRequest{})
		assert.ErrorIs(t, err, errBoom)
	})
	t.Run("folders", func(t *testing.T) {
		c, d := newController(t)
		d.stats.EXPECT().SearchUsers(ctx, "", int64(0), int64(0), DefaultUsersLimit+1).Return(users, nil)
		d.stats.EXPECT().CreditUsageByUser(ctx, []int64{1}, _since).Return(nil, nil)
		d.stats.EXPECT().LiveFoldersByUser(ctx, []int64{1}, _now).Return(nil, errBoom)
		_, err := c.ListUsers(ctx, &entity.ListAdminUsersRequest{})
		assert.ErrorIs(t, err, errBoom)
	})
}

func TestGetUser(t *testing.T) {
	ctx := context.Background()
	t.Run("found", func(t *testing.T) {
		c, d := newController(t)
		d.users.EXPECT().FindUserByID(ctx, int64(7)).Return(model.User{ID: 7, Email: "a@b.c"}, nil)
		d.stats.EXPECT().CreditUsageByUser(ctx, []int64{7}, _since).Return(map[int64]int{7: 12}, nil)
		d.stats.EXPECT().LiveFoldersByUser(ctx, []int64{7}, _now).Return(map[int64]int{7: 1}, nil)
		rs, err := c.GetUser(ctx, &entity.GetAdminUserRequest{UserID: 7})
		require.NoError(t, err)
		assert.Equal(t, "a@b.c", rs.User.Email)
		assert.Equal(t, 12, rs.User.Used)
		assert.Equal(t, 1, rs.User.Folders)
	})
	t.Run("not found", func(t *testing.T) {
		c, d := newController(t)
		d.users.EXPECT().FindUserByID(ctx, int64(7)).Return(model.User{}, repository.ErrNotFound)
		_, err := c.GetUser(ctx, &entity.GetAdminUserRequest{UserID: 7})
		assert.ErrorIs(t, err, ErrUserNotFound)
	})
	t.Run("find error", func(t *testing.T) {
		c, d := newController(t)
		d.users.EXPECT().FindUserByID(ctx, int64(7)).Return(model.User{}, errBoom)
		_, err := c.GetUser(ctx, &entity.GetAdminUserRequest{UserID: 7})
		assert.ErrorIs(t, err, errBoom)
	})
	t.Run("usage error", func(t *testing.T) {
		c, d := newController(t)
		d.users.EXPECT().FindUserByID(ctx, int64(7)).Return(model.User{ID: 7}, nil)
		d.stats.EXPECT().CreditUsageByUser(ctx, []int64{7}, _since).Return(nil, errBoom)
		_, err := c.GetUser(ctx, &entity.GetAdminUserRequest{UserID: 7})
		assert.ErrorIs(t, err, errBoom)
	})
}

func TestListUserCreditActivities(t *testing.T) {
	ctx := context.Background()
	rq := &entity.ListCreditActivitiesRequest{UserID: 7, Limit: 5}
	t.Run("found", func(t *testing.T) {
		c, d := newController(t)
		want := &entity.ListCreditActivitiesResponse{Next: 3}
		d.users.EXPECT().FindUserByID(ctx, int64(7)).Return(model.User{ID: 7}, nil)
		d.credits.EXPECT().ListCreditActivities(ctx, rq).Return(want, nil)
		rs, err := c.ListUserCreditActivities(ctx, rq)
		require.NoError(t, err)
		assert.Same(t, want, rs)
	})
	t.Run("not found", func(t *testing.T) {
		c, d := newController(t)
		d.users.EXPECT().FindUserByID(ctx, int64(7)).Return(model.User{}, repository.ErrNotFound)
		_, err := c.ListUserCreditActivities(ctx, rq)
		assert.ErrorIs(t, err, ErrUserNotFound)
	})
}
