package handlers

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	tgi18n "github.com/nikita/tg-linguine/internal/i18n"
	"github.com/nikita/tg-linguine/internal/screen"
	"github.com/nikita/tg-linguine/internal/session"
)

// Fallback handles text messages that are not commands, URLs, or API-key input.
type Fallback struct {
	mgr       *screen.Manager
	nav       *Nav
	keyWaiter *session.APIKeyWaiter
	log       *slog.Logger
}

// NewFallback returns a Fallback handler.
func NewFallback(mgr *screen.Manager, nav *Nav, keyWaiter *session.APIKeyWaiter, log *slog.Logger) *Fallback {
	return &Fallback{mgr: mgr, nav: nav, keyWaiter: keyWaiter, log: log}
}

// Match fires for text messages that are not commands, URLs, or API-key input.
func (f *Fallback) Match(u *models.Update) bool {
	if u.Message == nil || u.Message.From == nil || u.Message.Text == "" {
		return false
	}
	if strings.HasPrefix(u.Message.Text, "/") {
		return false
	}
	if MatchURLMessage(u) {
		return false
	}
	if f.keyWaiter.IsArmed(u.Message.From.ID) {
		return false
	}
	return true
}

// Handle sends a "use buttons" hint (auto-deleted after 5 s) and re-emits the
// active screen so it scrolls back into view after the user's stray message.
func (f *Fallback) Handle(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID := update.Message.Chat.ID
	loc := tgi18n.FromContext(ctx)

	// Send "use buttons" hint, schedule auto-delete in 5 seconds.
	hint, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   tgi18n.T(loc, "chat.use_buttons", nil),
	})
	if err == nil {
		hintID := hint.ID
		time.AfterFunc(5*time.Second, func() {
			_, _ = b.DeleteMessage(context.Background(), &bot.DeleteMessageParams{
				ChatID:    chatID,
				MessageID: hintID,
			})
		})
	}

	// Re-emit active screen as new message (old one scrolled up).
	activeID, _, screenCtx, found, _ := f.mgr.ActiveID(ctx, chatID)
	if !found {
		return
	}
	// Retire old active screen first, so nav.Render creates a new message.
	_ = f.mgr.RetireActive(ctx, b, chatID)
	f.nav.Render(ctx, b, activeID, chatID, screenCtx)
}
