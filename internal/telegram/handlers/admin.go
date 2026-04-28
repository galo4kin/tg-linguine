// Package-level: handlers for the single-admin commands /stats, /shutdown,
// /whoami. The IsAdmin gate is applied inside each handler — non-admins are
// silently ignored (no reply at all) so the commands' very existence stays
// invisible to regular users.
package handlers

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/nikita/tg-linguine/internal/articles"
	"github.com/nikita/tg-linguine/internal/dictionary"
	tgi18n "github.com/nikita/tg-linguine/internal/i18n"
	"github.com/nikita/tg-linguine/internal/users"
)

// AdminGate is the predicate the handler consults to decide whether the
// caller has admin rights. Injected so the handler doesn't depend on the
// `telegram` package (which depends on `handlers`) — this keeps the import
// graph acyclic.
type AdminGate func(userID int64) bool

type Admin struct {
	gate     AdminGate
	users    *users.Service
	articles articles.Repository
	dict     dictionary.Repository
	db       *sql.DB
	log      *slog.Logger
}

func NewAdmin(
	gate AdminGate,
	usersSvc *users.Service,
	articlesRepo articles.Repository,
	dictRepo dictionary.Repository,
	db *sql.DB,
	log *slog.Logger,
) *Admin {
	return &Admin{
		gate:     gate,
		users:    usersSvc,
		articles: articlesRepo,
		dict:     dictRepo,
		db:       db,
		log:      log,
	}
}

func (a *Admin) HandleStats(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message == nil || update.Message.From == nil {
		return
	}
	from := update.Message.From
	if !a.gate(from.ID) {
		return
	}

	chatID := update.Message.Chat.ID
	loc := tgi18n.FromContext(ctx)
	sendErr := func(label string, err error) {
		a.log.Error(label, "errors_total", 1, "err", err)
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   tgi18n.T(loc, "error.generic", nil),
		})
	}

	uStats, err := a.users.Stats(ctx)
	if err != nil {
		sendErr("admin /stats: users.Stats", err)
		return
	}
	totalArticles, err := a.articles.CountAll(ctx, a.db)
	if err != nil {
		sendErr("admin /stats: articles.CountAll", err)
		return
	}
	dayArticles, err := a.articles.CountSince(ctx, a.db, time.Now().Add(-24*time.Hour))
	if err != nil {
		sendErr("admin /stats: articles.CountSince", err)
		return
	}
	totalWords, err := a.dict.CountAll(ctx, a.db)
	if err != nil {
		sendErr("admin /stats: dict.CountAll", err)
		return
	}
	body := tgi18n.T(loc, "admin.stats", map[string]any{
		"Users":         uStats.Total,
		"Active24h":     uStats.Active24h,
		"Active7d":      uStats.Active7d,
		"Articles":      totalArticles,
		"Articles24h":   dayArticles,
		"DictionaryWords": totalWords,
	})
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   body,
	}); err != nil {
		a.log.Error("admin /stats: send", "errors_total", 1, "err", err)
	}
}

func (a *Admin) HandleWhoami(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message == nil || update.Message.From == nil {
		return
	}
	from := update.Message.From
	loc := tgi18n.FromContext(ctx)

	role := "admin.whoami_user"
	if a.gate(from.ID) {
		role = "admin.whoami_admin"
	}
	body := tgi18n.T(loc, role, map[string]any{"UserID": from.ID})
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   body,
	}); err != nil {
		a.log.Error("admin /whoami: send", "errors_total", 1, "err", err)
	}
}

// HandleShutdown logs, sends an ack to the admin, then exits the process.
// Watchdog (cron) will spin up a fresh pid within ≤1 minute. We use
// os.Exit(0) instead of orchestrating ctx cancellation back to main —
// matches the tg-boltun pattern and keeps the dependency surface tiny.
// `Exit` is the variable so tests can swap it for a no-op.
func (a *Admin) HandleShutdown(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message == nil || update.Message.From == nil {
		return
	}
	from := update.Message.From
	if !a.gate(from.ID) {
		return
	}

	a.log.Warn("admin requested shutdown",
		"user_id", from.ID,
	)
	loc := tgi18n.FromContext(ctx)
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   tgi18n.T(loc, "admin.shutdown_ack", nil),
	}); err != nil {
		a.log.Error("admin /shutdown: send ack", "errors_total", 1, "err", err)
	}
	Exit(0)
}

// Exit is a hook so tests can stub the real process exit. Production code
// path is the unmodified os.Exit.
var Exit = func(code int) {
	os.Exit(code)
}
