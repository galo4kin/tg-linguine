package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/nikita/tg-linguine/internal/screen"
)

// ScreenRenderer renders a named screen into chat chatID using the provided bot.
type ScreenRenderer func(ctx context.Context, b *bot.Bot, chatID int64, screenCtx map[string]string)

// Nav handles nav:home and nav:back:* callback queries.
type Nav struct {
	mgr       *screen.Manager
	renderers map[screen.ScreenID]ScreenRenderer
	log       *slog.Logger
}

// NewNav returns a Nav backed by mgr.
func NewNav(mgr *screen.Manager, log *slog.Logger) *Nav {
	return &Nav{mgr: mgr, renderers: make(map[screen.ScreenID]ScreenRenderer), log: log}
}

// Register associates a renderer with a screen ID.
func (n *Nav) Register(id screen.ScreenID, r ScreenRenderer) { n.renderers[id] = r }

// Render calls the registered renderer for id, if any.
func (n *Nav) Render(ctx context.Context, b *bot.Bot, id screen.ScreenID, chatID int64, screenCtx map[string]string) {
	r, ok := n.renderers[id]
	if !ok {
		n.log.Warn("nav: no renderer registered", "screen_id", id)
		return
	}
	r(ctx, b, chatID, screenCtx)
}

// HandleCallback processes nav:home and nav:back:<screenID>:<ctxJSON> callbacks.
func (n *Nav) HandleCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	cq := update.CallbackQuery
	if cq == nil {
		return
	}
	b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: cq.ID})
	chatID, _, ok := callbackMessageRef(cq)
	if !ok {
		return
	}
	data := strings.TrimPrefix(cq.Data, screen.CallbackPrefixNav)
	switch {
	case data == "home":
		n.Render(ctx, b, screen.ScreenWelcome, chatID, nil)
	case strings.HasPrefix(data, "back:"):
		rest := strings.TrimPrefix(data, "back:")
		sep := strings.Index(rest, ":")
		if sep < 0 {
			n.log.Warn("nav back malformed", "data", cq.Data)
			return
		}
		sid := screen.ScreenID(rest[:sep])
		var screenCtx map[string]string
		_ = json.Unmarshal([]byte(rest[sep+1:]), &screenCtx)
		n.Render(ctx, b, sid, chatID, screenCtx)
	default:
		n.log.Warn("nav unknown action", "data", cq.Data)
	}
}
