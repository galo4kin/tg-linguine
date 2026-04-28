package telegram

import "github.com/nikita/tg-linguine/internal/config"

// IsAdmin reports whether the given Telegram user id matches the configured
// admin. When `cfg.AdminUserID == 0` (unset env), admin functionality is
// off and IsAdmin always returns false — no user can claim admin rights by
// guessing zero.
func IsAdmin(cfg *config.Config, userID int64) bool {
	if cfg == nil || cfg.AdminUserID == 0 {
		return false
	}
	return userID == cfg.AdminUserID
}
