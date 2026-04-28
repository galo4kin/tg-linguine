DROP INDEX IF EXISTS idx_users_last_seen;
ALTER TABLE users DROP COLUMN last_seen_at;
