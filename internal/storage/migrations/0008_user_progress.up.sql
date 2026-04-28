-- Per-user gamification state: total XP, day-streak with longest, today's
-- correct counter and a once-per-day "goal hit" flag so the bonus is paid
-- at most once.
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
