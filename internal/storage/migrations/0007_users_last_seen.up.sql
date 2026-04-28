ALTER TABLE users ADD COLUMN last_seen_at DATETIME DEFAULT NULL;
CREATE INDEX idx_users_last_seen ON users(last_seen_at);
