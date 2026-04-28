package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// MessageSender is the slice of the go-telegram client that SendStartupPing
// needs. Production wires the real *bot.Bot; tests inject a fake.
type MessageSender interface {
	SendMessage(ctx context.Context, params *bot.SendMessageParams) (*models.Message, error)
}

// SendStartupPing posts a one-line "bot is up" message to the admin chat.
// When adminUserID == 0 (admin disabled) it returns immediately — no Telegram
// call, no log line. Send failures are logged at WARN and never fail the
// process.
//
// If extra event sources appear later (LLM error notifications, etc.) wrap
// them in a throttled helper modelled on tg-boltun/instagram.go's mutex +
// lastNotify pattern; this function only handles the once-per-process boot
// ping.
func SendStartupPing(ctx context.Context, sender MessageSender, log *slog.Logger, adminUserID int64, version, commit string, startedAt time.Time) {
	if adminUserID == 0 {
		return
	}
	text := fmt.Sprintf("🍝 tg-linguine поднялся\nversion: %s\ncommit: %s\nstarted: %s",
		version, commit, startedAt.Format(time.RFC3339))
	if _, err := sender.SendMessage(ctx, &bot.SendMessageParams{ChatID: adminUserID, Text: text}); err != nil {
		log.Warn("admin startup ping failed", "err", err)
	}
}

// SendStartupPing dispatches the admin boot notification through the bot's
// own API client. Convenience wrapper so cmd/bot/main.go does not have to
// reach into tb.b.
func (tb *Bot) SendStartupPing(ctx context.Context, adminUserID int64, version, commit string, startedAt time.Time) {
	SendStartupPing(ctx, tb.b, tb.log, adminUserID, version, commit, startedAt)
}

// BuildInfo derives the version/commit pair displayed in the startup ping
// from runtime/debug.ReadBuildInfo(). The module's own version is preferred
// when the binary was tagged; otherwise the caller-supplied fallback (the
// `version` global in main, default "dev") is kept. The commit is the short
// (7-char) form of vcs.revision; "unknown" if the build did not record one.
func BuildInfo(fallbackVersion string) (version, commit string) {
	version = fallbackVersion
	commit = "unknown"
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return version, commit
	}
	if (version == "" || version == "dev") && info.Main.Version != "" && info.Main.Version != "(devel)" {
		version = info.Main.Version
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && s.Value != "" {
			if len(s.Value) >= 7 {
				commit = s.Value[:7]
			} else {
				commit = s.Value
			}
		}
	}
	return version, commit
}
