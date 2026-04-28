# 53 — Хранилище геймификации (XP, стрик дней, дневная цель)

## Цель
Добавить per-user состояние для XP, стрика дней и дневной цели. Сама бизнес-логика подключения к квизу — в шаге 54.

## Что сделать
- Миграция `internal/storage/migrations/0006_user_progress.up.sql` (+ `down`):
  ```sql
  CREATE TABLE user_progress (
    user_id INTEGER PRIMARY KEY,
    xp_total INTEGER NOT NULL DEFAULT 0,
    day_streak INTEGER NOT NULL DEFAULT 0,
    longest_streak INTEGER NOT NULL DEFAULT 0,
    last_active_date DATE,
    today_correct INTEGER NOT NULL DEFAULT 0,
    today_date DATE,
    daily_goal_hit_today INTEGER NOT NULL DEFAULT 0,  -- 0/1, чтобы бонус начислять один раз в день
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
  );
  ```
- Конфиг (`internal/config`): `QUIZ_DAILY_GOAL` (default 20), `QUIZ_XP_PER_CORRECT` (default 10), `QUIZ_XP_BONUS_GOAL` (default 50).
- Новый пакет `internal/progress`:
  - `Repository` с методами:
    - `Get(ctx, db, userID) (*UserProgress, error)`;
    - `RolloverIfNewDay(ctx, db, userID, today time.Time)` — обновляет стрик: `today == last_active_date+1` → `+1` и обновляет `longest`; `today == last_active_date` → noop; иначе → `1` начнёт со следующего правильного ответа (логика такая: пока пользователь не дал ни одного правильного ответа сегодня, стрик не обновляем; при первом правильном за день — обновляем; см. шаг 54).
    - `RecordCorrect(ctx, db, userID, today time.Time, xpPerCorrect, dailyGoal, xpBonus int) (RecordResult, error)` где `RecordResult` содержит `XPGained`, `GoalJustHit bool`, `NewDayStreak int`.
    - `RecordWrong(ctx, db, userID, today time.Time)` — без ущерба стрику дней.
- Юнит-тесты: rollover на стыке дней; первый правильный ответ за день; пропуск дня; повторный «удар» по цели не начисляет бонус повторно.

## Definition of Done
- Миграция применяется в обе стороны.
- Тесты на rollover-логику зелёные.
- `make build` / `make test`.
- Один коммит `step 53: gamification-storage`.
