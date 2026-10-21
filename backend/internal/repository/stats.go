// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package repository

//go:generate mockgen -source=stats.go -destination=mocks/stats.go -package=mocks

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/tmphost/tmphost/backend/internal/data/model"
)

// _usageOps are the ops that count as credit usage. A []uint8 would be
// bound as one blob, so they are ints.
var _usageOps = []int{int(model.CreditOpSpend), int(model.CreditOpRefund)}

type (
	// StatsRepository answers the admin page's questions about every
	// account. It only reads.
	StatsRepository interface {
		// Totals counts the users, the credits they hold and the folders
		// live at now.
		Totals(ctx context.Context, now time.Time) (Totals, error)
		// DailyCreditUsage returns, per UTC day since since, the credits
		// spends took net of refunds. Days without any are left out.
		DailyCreditUsage(ctx context.Context, since time.Time) ([]DayCount, error)
		// DailySignups returns, per UTC day since since, how many users
		// registered. Days without any are left out.
		DailySignups(ctx context.Context, since time.Time) ([]DayCount, error)
		// SearchUsers returns up to limit users, newest first, with IDs
		// below before (0: from the newest). A non-empty q keeps the users
		// whose email or name contains it, ignoring case, and the user
		// whose ID is id.
		SearchUsers(ctx context.Context, q string, id, before int64, limit int) ([]model.User, error)
		// CreditUsageByUser returns the credits each of the users spent
		// since since, net of refunds. Users who spent none are left out.
		CreditUsageByUser(ctx context.Context, userIDs []int64, since time.Time) (map[int64]int, error)
		// LiveFoldersByUser returns how many folders each of the users has
		// live at now. Users without any are left out.
		LiveFoldersByUser(ctx context.Context, userIDs []int64, now time.Time) (map[int64]int, error)
	}

	// Totals are counts over every account.
	Totals struct {
		Users   int64
		Credits int64
		Folders int64
	}

	// DayCount is a count for one UTC day, YYYY-MM-DD.
	DayCount struct {
		Day   string
		Count int64
	}

	statsRepository struct {
		db *gorm.DB
	}

	userCount struct {
		UserID int64
		Count  int
	}
)

// NewStatsRepository returns a GORM-backed stats repository.
func NewStatsRepository(db *gorm.DB) StatsRepository {
	return &statsRepository{db: db}
}

// dayExpr is the SQL expression for the UTC day (YYYY-MM-DD text) of a
// time column. SQLite stores times as text with their offset, which its
// date() turns into UTC; Postgres needs the zone named.
func dayExpr(dialect, column string) string {
	if dialect == "postgres" {
		return "to_char(" + column + " AT TIME ZONE 'UTC', 'YYYY-MM-DD')"
	}
	return "date(" + column + ")"
}

func (r *statsRepository) Totals(ctx context.Context, now time.Time) (Totals, error) {
	var t Totals
	db := r.db.WithContext(ctx)
	err := db.Model(&model.User{}).Select("COUNT(*) AS users, COALESCE(SUM(credits), 0) AS credits").Scan(&t).Error
	if err != nil {
		return Totals{}, err
	}
	if err := db.Model(&model.Folder{}).Where("expires_at > ?", now).Count(&t.Folders).Error; err != nil {
		return Totals{}, err
	}
	return t, nil
}

func (r *statsRepository) DailyCreditUsage(ctx context.Context, since time.Time) ([]DayCount, error) {
	day := dayExpr(r.db.Name(), "created_at")
	var ds []DayCount
	err := r.db.WithContext(ctx).Model(&model.CreditActivity{}).
		Select(day+" AS day, -SUM(change) AS count").
		Where("created_at >= ? AND ops IN ?", since, _usageOps).
		Group(day).Order("day").
		Scan(&ds).Error
	if err != nil {
		return nil, err
	}
	return ds, nil
}

func (r *statsRepository) DailySignups(ctx context.Context, since time.Time) ([]DayCount, error) {
	day := dayExpr(r.db.Name(), "created_at")
	var ds []DayCount
	err := r.db.WithContext(ctx).Model(&model.User{}).
		Select(day+" AS day, COUNT(*) AS count").
		Where("created_at >= ?", since).
		Group(day).Order("day").
		Scan(&ds).Error
	if err != nil {
		return nil, err
	}
	return ds, nil
}

func (r *statsRepository) SearchUsers(ctx context.Context, q string, id, before int64, limit int) ([]model.User, error) {
	db := r.db.WithContext(ctx)
	if before > 0 {
		db = db.Where("id < ?", before)
	}
	if q != "" {
		like := "%" + escapeLike(strings.ToLower(q)) + "%"
		db = db.Where("LOWER(email) LIKE ? ESCAPE '\\' OR LOWER(name) LIKE ? ESCAPE '\\' OR id = ?", like, like, id)
	}
	var users []model.User
	if err := db.Order("id DESC").Limit(limit).Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}

// escapeLike makes s match itself literally in a LIKE pattern.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(s)
}

func (r *statsRepository) CreditUsageByUser(ctx context.Context, userIDs []int64, since time.Time) (map[int64]int, error) {
	return r.countByUser(r.db.WithContext(ctx).Model(&model.CreditActivity{}).
		Select("user_id, -SUM(change) AS count").
		Where("user_id IN ? AND created_at >= ? AND ops IN ?", userIDs, since, _usageOps))
}

func (r *statsRepository) LiveFoldersByUser(ctx context.Context, userIDs []int64, now time.Time) (map[int64]int, error) {
	return r.countByUser(r.db.WithContext(ctx).Model(&model.Folder{}).
		Select("user_id, COUNT(*) AS count").
		Where("user_id IN ? AND expires_at > ?", userIDs, now))
}

// countByUser runs a query selecting user_id and count, grouped by user.
func (r *statsRepository) countByUser(q *gorm.DB) (map[int64]int, error) {
	var rows []userCount
	if err := q.Group("user_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	counts := make(map[int64]int, len(rows))
	for _, row := range rows {
		if row.Count != 0 {
			counts[row.UserID] = row.Count
		}
	}
	return counts, nil
}
