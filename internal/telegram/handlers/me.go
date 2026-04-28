package handlers

import (
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	tgi18n "github.com/nikita/tg-linguine/internal/i18n"
	"github.com/nikita/tg-linguine/internal/progress"
	"github.com/nikita/tg-linguine/internal/users"
)

// Me serves the /me command — a single message with the user's active
// language, level, total XP, day-streak, and progress toward today's
// goal.
type Me struct {
	users     *users.Service
	languages users.UserLanguageRepository
	progress  progress.Repository
	dailyGoal int
	db        *sql.DB
	bundle    *goi18n.Bundle
	log       *slog.Logger
}

func NewMe(
	svc *users.Service,
	langs users.UserLanguageRepository,
	prog progress.Repository,
	dailyGoal int,
	db *sql.DB,
	bundle *goi18n.Bundle,
	log *slog.Logger,
) *Me {
	return &Me{
		users:     svc,
		languages: langs,
		progress:  prog,
		dailyGoal: dailyGoal,
		db:        db,
		bundle:    bundle,
		log:       log,
	}
}

// HandleCommand renders the profile in the user's interface language.
func (h *Me) HandleCommand(ctx context.Context, b *bot.Bot, update *models.Update) {
	msg := update.Message
	if msg == nil || msg.From == nil {
		return
	}
	u, ok := resolveMessageUser(ctx, h.users, msg, h.log, "me cmd")
	if !ok {
		return
	}
	loc := tgi18n.For(h.bundle, u.InterfaceLanguage)

	// Roll over so today_correct reflects today's tally even if the user
	// has only opened /me without finishing a quiz.
	if err := h.progress.RolloverIfNewDay(ctx, h.db, u.ID, time.Now()); err != nil {
		h.log.Error("me: rollover", "err", err)
	}
	prog, err := h.progress.Get(ctx, h.db, u.ID)
	if err != nil {
		h.log.Error("me: get progress", "err", err)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: msg.Chat.ID,
			Text:   tgi18n.T(loc, "error.generic", nil),
		})
		return
	}

	active, err := h.languages.Active(ctx, u.ID)
	if err != nil {
		h.log.Error("me: active language", "err", err)
	}

	var sb strings.Builder
	sb.WriteString(tgi18n.T(loc, "me.title", nil))
	sb.WriteString("\n")
	if active != nil {
		sb.WriteString(tgi18n.T(loc, "me.active_language", map[string]any{
			"Language": active.LanguageCode,
			"Level":    active.CEFRLevel,
		}))
		sb.WriteString("\n")
	}
	sb.WriteString(tgi18n.T(loc, "me.xp", map[string]any{"XP": prog.XPTotal}))
	sb.WriteString("\n")
	sb.WriteString(tgi18n.T(loc, "me.streak", map[string]any{
		"Streak":  prog.DayStreak,
		"Longest": prog.LongestStreak,
	}))
	sb.WriteString("\n")
	sb.WriteString(tgi18n.T(loc, "me.goal", map[string]any{
		"Today": prog.TodayCorrect,
		"Goal":  h.dailyGoal,
	}))

	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: msg.Chat.ID,
		Text:   strings.TrimRight(sb.String(), "\n"),
	}); err != nil {
		h.log.Debug("me: send", "err", err)
	}
}
