// Package progress is the per-user gamification state — XP, day-streak,
// daily goal tracking. The handler layer (`/study`, `/me`) reads and
// writes this state, but the rules around streak rollover and the
// once-a-day bonus live here so they can be unit-tested without a bot.
package progress

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// UserProgress is one row from the user_progress table.
type UserProgress struct {
	UserID            int64
	XPTotal           int
	DayStreak         int
	LongestStreak     int
	LastActiveDate    string // YYYY-MM-DD; empty when never recorded
	TodayCorrect      int
	TodayDate         string // YYYY-MM-DD; empty until first activity
	DailyGoalHitToday bool
}

// RecordResult summarises what changed during a RecordCorrect call so
// the caller can render feedback without fetching the row again.
// XPGained == BaseXP + BonusXP; the split is exposed so the UI can
// celebrate the bonus separately from the per-correct payout.
type RecordResult struct {
	BaseXP       int
	BonusXP      int
	XPGained     int
	GoalJustHit  bool
	NewDayStreak int
	NewXPTotal   int
	TodayCorrect int
}

// Repository hides the SQL behind a small interface so handlers can
// stay decoupled from the storage layer in tests.
type Repository interface {
	Get(ctx context.Context, db DB, userID int64) (*UserProgress, error)
	RolloverIfNewDay(ctx context.Context, db DB, userID int64, today time.Time) error
	RecordCorrect(ctx context.Context, db DB, userID int64, today time.Time, xpPerCorrect, dailyGoal, xpBonus int) (RecordResult, error)
	RecordWrong(ctx context.Context, db DB, userID int64, today time.Time) error
}

// DB is the subset of *sql.DB / *sql.Tx the repository needs. Mirrors
// the pattern used by dictionary.UserWordStatusRepository so callers
// can pass either.
type DB interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

const dateLayout = "2006-01-02"

// SQLite is the default Repository backed by a *sql.DB.
type SQLite struct{}

// NewSQLite builds the SQLite-backed repository.
func NewSQLite() *SQLite { return &SQLite{} }

// Get returns the row for userID, creating an empty one on first read so
// the caller never has to handle the "no row yet" case.
func (s *SQLite) Get(ctx context.Context, db DB, userID int64) (*UserProgress, error) {
	if err := ensureRow(ctx, db, userID); err != nil {
		return nil, err
	}
	const q = `
		SELECT user_id, xp_total, day_streak, longest_streak,
		       COALESCE(last_active_date, ''),
		       today_correct,
		       COALESCE(today_date, ''),
		       daily_goal_hit_today
		FROM user_progress
		WHERE user_id = ?
	`
	var p UserProgress
	var goal int
	if err := db.QueryRowContext(ctx, q, userID).Scan(
		&p.UserID, &p.XPTotal, &p.DayStreak, &p.LongestStreak,
		&p.LastActiveDate, &p.TodayCorrect, &p.TodayDate, &goal,
	); err != nil {
		return nil, fmt.Errorf("progress: get: %w", err)
	}
	p.DailyGoalHitToday = goal != 0
	return &p, nil
}

// RolloverIfNewDay resets today_correct / daily_goal_hit_today when the
// stored today_date is not the current day. The day-streak itself is
// NOT touched here — it advances on the first correct answer of a day
// (see RecordCorrect), so that opening /study without answering keeps
// the streak unchanged.
func (s *SQLite) RolloverIfNewDay(ctx context.Context, db DB, userID int64, today time.Time) error {
	if err := ensureRow(ctx, db, userID); err != nil {
		return err
	}
	day := today.Format(dateLayout)
	const q = `
		UPDATE user_progress
		SET today_correct = 0,
		    daily_goal_hit_today = 0,
		    today_date = ?
		WHERE user_id = ?
		  AND (today_date IS NULL OR today_date <> ?)
	`
	if _, err := db.ExecContext(ctx, q, day, userID, day); err != nil {
		return fmt.Errorf("progress: rollover: %w", err)
	}
	return nil
}

// RecordCorrect awards XP for a correct answer, advances the day-streak
// when this is the first correct answer of `today`, and pays out the
// bonus XP exactly once per day when today_correct first reaches
// dailyGoal. The caller passes the configured XP/goal values so tests
// can pin them.
func (s *SQLite) RecordCorrect(
	ctx context.Context, db DB, userID int64, today time.Time,
	xpPerCorrect, dailyGoal, xpBonus int,
) (RecordResult, error) {
	if err := ensureRow(ctx, db, userID); err != nil {
		return RecordResult{}, err
	}
	if err := s.RolloverIfNewDay(ctx, db, userID, today); err != nil {
		return RecordResult{}, err
	}

	cur, err := s.Get(ctx, db, userID)
	if err != nil {
		return RecordResult{}, err
	}

	day := today.Format(dateLayout)
	newDayStreak := cur.DayStreak
	newLongest := cur.LongestStreak
	// Day-streak advances on the FIRST correct answer of the day.
	if cur.LastActiveDate != day {
		switch cur.LastActiveDate {
		case "":
			newDayStreak = 1
		case today.AddDate(0, 0, -1).Format(dateLayout):
			newDayStreak = cur.DayStreak + 1
		default:
			newDayStreak = 1
		}
		if newDayStreak > newLongest {
			newLongest = newDayStreak
		}
	}

	newTodayCorrect := cur.TodayCorrect + 1
	baseXP := xpPerCorrect
	bonusXP := 0
	goalJustHit := false
	if !cur.DailyGoalHitToday && dailyGoal > 0 && newTodayCorrect >= dailyGoal {
		goalJustHit = true
		bonusXP = xpBonus
	}
	xp := baseXP + bonusXP
	newGoalHit := cur.DailyGoalHitToday || goalJustHit

	const q = `
		UPDATE user_progress
		SET xp_total = xp_total + ?,
		    day_streak = ?,
		    longest_streak = ?,
		    last_active_date = ?,
		    today_correct = ?,
		    today_date = ?,
		    daily_goal_hit_today = ?
		WHERE user_id = ?
	`
	if _, err := db.ExecContext(ctx, q,
		xp, newDayStreak, newLongest, day, newTodayCorrect, day, boolToInt(newGoalHit), userID,
	); err != nil {
		return RecordResult{}, fmt.Errorf("progress: record correct: %w", err)
	}

	return RecordResult{
		BaseXP:       baseXP,
		BonusXP:      bonusXP,
		XPGained:     xp,
		GoalJustHit:  goalJustHit,
		NewDayStreak: newDayStreak,
		NewXPTotal:   cur.XPTotal + xp,
		TodayCorrect: newTodayCorrect,
	}, nil
}

// RecordWrong is a no-op for the streak/XP — wrong answers do not break
// the day-streak. We still rollover so the row's today_date is current.
func (s *SQLite) RecordWrong(ctx context.Context, db DB, userID int64, today time.Time) error {
	if err := ensureRow(ctx, db, userID); err != nil {
		return err
	}
	return s.RolloverIfNewDay(ctx, db, userID, today)
}

// ensureRow inserts a zero row for userID if none exists. Idempotent.
func ensureRow(ctx context.Context, db DB, userID int64) error {
	const q = `INSERT OR IGNORE INTO user_progress(user_id) VALUES (?)`
	if _, err := db.ExecContext(ctx, q, userID); err != nil {
		return fmt.Errorf("progress: ensure row: %w", err)
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
