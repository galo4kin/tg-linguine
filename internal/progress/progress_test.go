package progress_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/nikita/tg-linguine/internal/progress"
)

const schemaSQL = `
CREATE TABLE users (id INTEGER PRIMARY KEY);
CREATE TABLE user_progress (
  user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  xp_total INTEGER NOT NULL DEFAULT 0,
  day_streak INTEGER NOT NULL DEFAULT 0,
  longest_streak INTEGER NOT NULL DEFAULT 0,
  last_active_date DATE,
  today_correct INTEGER NOT NULL DEFAULT 0,
  today_date DATE,
  daily_goal_hit_today INTEGER NOT NULL DEFAULT 0
);
`

func openTestDB(t *testing.T) (*sql.DB, int64) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("fk: %v", err)
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		t.Fatalf("schema: %v", err)
	}
	res, err := db.Exec(`INSERT INTO users DEFAULT VALUES`)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	id, _ := res.LastInsertId()
	return db, id
}

func mustDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatalf("parse date: %v", err)
	}
	return d
}

func TestGetCreatesEmptyRow(t *testing.T) {
	db, uid := openTestDB(t)
	repo := progress.NewSQLite()
	p, err := repo.Get(context.Background(), db, uid)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if p.XPTotal != 0 || p.DayStreak != 0 || p.LongestStreak != 0 {
		t.Fatalf("expected empty row, got %+v", p)
	}
}

func TestRecordCorrect_FirstAnswerStartsStreakAtOne(t *testing.T) {
	db, uid := openTestDB(t)
	repo := progress.NewSQLite()
	day := mustDate(t, "2026-04-28")
	res, err := repo.RecordCorrect(context.Background(), db, uid, day, 10, 20, 50)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if res.XPGained != 10 || res.NewDayStreak != 1 || res.GoalJustHit {
		t.Fatalf("unexpected result: %+v", res)
	}
	p, _ := repo.Get(context.Background(), db, uid)
	if p.DayStreak != 1 || p.LongestStreak != 1 || p.XPTotal != 10 {
		t.Fatalf("row: %+v", p)
	}
}

func TestRecordCorrect_ConsecutiveDayIncrementsStreak(t *testing.T) {
	db, uid := openTestDB(t)
	repo := progress.NewSQLite()
	if _, err := repo.RecordCorrect(context.Background(), db, uid, mustDate(t, "2026-04-27"), 10, 20, 50); err != nil {
		t.Fatalf("day1: %v", err)
	}
	res, err := repo.RecordCorrect(context.Background(), db, uid, mustDate(t, "2026-04-28"), 10, 20, 50)
	if err != nil {
		t.Fatalf("day2: %v", err)
	}
	if res.NewDayStreak != 2 {
		t.Fatalf("expected streak=2, got %d", res.NewDayStreak)
	}
}

func TestRecordCorrect_SkippedDayResetsToOne(t *testing.T) {
	db, uid := openTestDB(t)
	repo := progress.NewSQLite()
	if _, err := repo.RecordCorrect(context.Background(), db, uid, mustDate(t, "2026-04-25"), 10, 20, 50); err != nil {
		t.Fatalf("day1: %v", err)
	}
	res, err := repo.RecordCorrect(context.Background(), db, uid, mustDate(t, "2026-04-28"), 10, 20, 50)
	if err != nil {
		t.Fatalf("later: %v", err)
	}
	if res.NewDayStreak != 1 {
		t.Fatalf("expected streak reset to 1, got %d", res.NewDayStreak)
	}
	p, _ := repo.Get(context.Background(), db, uid)
	if p.LongestStreak != 1 {
		t.Fatalf("longest must be retained as max(1,1) = 1, got %d", p.LongestStreak)
	}
}

func TestRecordCorrect_SecondAnswerSameDayDoesNotChangeStreak(t *testing.T) {
	db, uid := openTestDB(t)
	repo := progress.NewSQLite()
	day := mustDate(t, "2026-04-28")
	if _, err := repo.RecordCorrect(context.Background(), db, uid, day, 10, 20, 50); err != nil {
		t.Fatalf("first: %v", err)
	}
	res, err := repo.RecordCorrect(context.Background(), db, uid, day, 10, 20, 50)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if res.NewDayStreak != 1 {
		t.Fatalf("streak must stay at 1 within same day, got %d", res.NewDayStreak)
	}
	if res.XPGained != 10 {
		t.Fatalf("xp must be plain xpPerCorrect, got %d", res.XPGained)
	}
}

func TestRecordCorrect_GoalHitOnceThenNoBonus(t *testing.T) {
	db, uid := openTestDB(t)
	repo := progress.NewSQLite()
	day := mustDate(t, "2026-04-28")
	const goal = 3
	for i := 1; i < goal; i++ {
		res, err := repo.RecordCorrect(context.Background(), db, uid, day, 10, goal, 50)
		if err != nil {
			t.Fatalf("answer %d: %v", i, err)
		}
		if res.GoalJustHit {
			t.Fatalf("answer %d hit goal too early", i)
		}
		if res.XPGained != 10 {
			t.Fatalf("answer %d xp=%d", i, res.XPGained)
		}
	}
	res, err := repo.RecordCorrect(context.Background(), db, uid, day, 10, goal, 50)
	if err != nil {
		t.Fatalf("goal answer: %v", err)
	}
	if !res.GoalJustHit || res.XPGained != 60 {
		t.Fatalf("goal-hit answer: %+v (want GoalJustHit, xp=60)", res)
	}
	res, err = repo.RecordCorrect(context.Background(), db, uid, day, 10, goal, 50)
	if err != nil {
		t.Fatalf("after goal: %v", err)
	}
	if res.GoalJustHit || res.XPGained != 10 {
		t.Fatalf("after goal: bonus must not repeat, got %+v", res)
	}
}

func TestRolloverResetsTodayCounters(t *testing.T) {
	db, uid := openTestDB(t)
	repo := progress.NewSQLite()
	day := mustDate(t, "2026-04-28")
	if _, err := repo.RecordCorrect(context.Background(), db, uid, day, 10, 20, 50); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := repo.RolloverIfNewDay(context.Background(), db, uid, mustDate(t, "2026-04-29")); err != nil {
		t.Fatalf("rollover: %v", err)
	}
	p, _ := repo.Get(context.Background(), db, uid)
	if p.TodayCorrect != 0 || p.DailyGoalHitToday {
		t.Fatalf("today counters must reset: %+v", p)
	}
	// Streak/XP are preserved across days.
	if p.DayStreak != 1 || p.XPTotal != 10 {
		t.Fatalf("rollover must not touch streak/xp: %+v", p)
	}
}

func TestRecordWrong_DoesNotChangeStreakOrXP(t *testing.T) {
	db, uid := openTestDB(t)
	repo := progress.NewSQLite()
	day := mustDate(t, "2026-04-28")
	if _, err := repo.RecordCorrect(context.Background(), db, uid, day, 10, 20, 50); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := repo.RecordWrong(context.Background(), db, uid, day); err != nil {
		t.Fatalf("wrong: %v", err)
	}
	p, _ := repo.Get(context.Background(), db, uid)
	if p.DayStreak != 1 || p.XPTotal != 10 || p.TodayCorrect != 1 {
		t.Fatalf("wrong must not move streak/xp/today_correct: %+v", p)
	}
}
