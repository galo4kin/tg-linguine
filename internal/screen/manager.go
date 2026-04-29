package screen

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// Store is the persistence layer Manager needs. internal/storage.ActiveScreenRepo
// satisfies this interface.
type Store interface {
	Set(ctx context.Context, chatID int64, messageID int, screenID, contextJSON string) error
	Get(ctx context.Context, chatID int64) (messageID int, screenID, contextJSON string, found bool, err error)
	Clear(ctx context.Context, chatID int64) error
}

// Sender is the subset of *bot.Bot Manager uses. Tests stub this.
type Sender interface {
	SendMessage(ctx context.Context, p *bot.SendMessageParams) (*models.Message, error)
	EditMessageText(ctx context.Context, p *bot.EditMessageTextParams) (*models.Message, error)
	EditMessageReplyMarkup(ctx context.Context, p *bot.EditMessageReplyMarkupParams) (*models.Message, error)
	DeleteMessage(ctx context.Context, p *bot.DeleteMessageParams) (bool, error)
}

// Manager manages the single "active screen" per chat.
type Manager struct {
	store Store
	log   *slog.Logger
}

// NewManager returns a Manager backed by store.
func NewManager(store Store, log *slog.Logger) *Manager {
	return &Manager{store: store, log: log}
}

// Show: if active screen has same ID — edit it; otherwise retire old and send new.
func (m *Manager) Show(ctx context.Context, b Sender, chatID int64, s Screen) error {
	activeMsgID, activeID, _, found, err := m.store.Get(ctx, chatID)
	if err != nil {
		return err
	}
	if found && ScreenID(activeID) == s.ID {
		return m.editActive(ctx, b, chatID, activeMsgID, s)
	}
	if found {
		m.retireMessage(ctx, b, chatID, activeMsgID)
	}
	return m.sendNew(ctx, b, chatID, s)
}

// Replace: always retire old and send new, regardless of screen ID.
// Use after free-text messages to ensure a clean slate.
func (m *Manager) Replace(ctx context.Context, b Sender, chatID int64, s Screen) error {
	activeMsgID, _, _, found, err := m.store.Get(ctx, chatID)
	if err != nil {
		return err
	}
	if found {
		m.retireMessage(ctx, b, chatID, activeMsgID)
	}
	return m.sendNew(ctx, b, chatID, s)
}

// EditInPlace: edit the active screen unconditionally.
// For status→card transitions (e.g. Analyzing → ArticleCard).
// Falls back to sendNew if no active screen is recorded.
func (m *Manager) EditInPlace(ctx context.Context, b Sender, chatID int64, s Screen) error {
	activeMsgID, _, _, found, err := m.store.Get(ctx, chatID)
	if err != nil {
		return err
	}
	if !found {
		return m.sendNew(ctx, b, chatID, s)
	}
	return m.editActive(ctx, b, chatID, activeMsgID, s)
}

// RetireActive strips buttons from the active screen and clears it from store.
func (m *Manager) RetireActive(ctx context.Context, b Sender, chatID int64) error {
	activeMsgID, _, _, found, err := m.store.Get(ctx, chatID)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	m.retireMessage(ctx, b, chatID, activeMsgID)
	return m.store.Clear(ctx, chatID)
}

// ActiveID returns the current active screen's ID, message ID, context, and
// whether one exists.
func (m *Manager) ActiveID(ctx context.Context, chatID int64) (ScreenID, int, map[string]string, bool, error) {
	msgID, sid, ctxJSON, found, err := m.store.Get(ctx, chatID)
	if err != nil || !found {
		return "", 0, nil, false, err
	}
	var c map[string]string
	if ctxJSON != "" {
		_ = json.Unmarshal([]byte(ctxJSON), &c)
	}
	return ScreenID(sid), msgID, c, true, nil
}

// sendNew sends a new message and records it in the store.
func (m *Manager) sendNew(ctx context.Context, b Sender, chatID int64, s Screen) error {
	msg, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        s.Text,
		ParseMode:   s.ParseMode,
		ReplyMarkup: s.Keyboard,
	})
	if err != nil {
		return err
	}
	ctxJSON, _ := json.Marshal(s.Context)
	return m.store.Set(ctx, chatID, msg.ID, string(s.ID), string(ctxJSON))
}

// editActive edits the existing active message. Falls back to sendNew on error.
func (m *Manager) editActive(ctx context.Context, b Sender, chatID int64, msgID int, s Screen) error {
	_, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      chatID,
		MessageID:   msgID,
		Text:        s.Text,
		ParseMode:   s.ParseMode,
		ReplyMarkup: s.Keyboard,
	})
	if err != nil {
		// Message may be deleted or too old — fall back to sending a fresh one.
		m.log.Warn("screen edit failed, sending new", "chat_id", chatID, "msg_id", msgID, "err", err)
		return m.sendNew(ctx, b, chatID, s)
	}
	ctxJSON, _ := json.Marshal(s.Context)
	return m.store.Set(ctx, chatID, msgID, string(s.ID), string(ctxJSON))
}

// retireMessage strips the inline keyboard from a message. Best-effort:
// errors are logged at Debug level and not returned.
func (m *Manager) retireMessage(ctx context.Context, b Sender, chatID int64, msgID int) {
	_, err := b.EditMessageReplyMarkup(ctx, &bot.EditMessageReplyMarkupParams{
		ChatID:      chatID,
		MessageID:   msgID,
		ReplyMarkup: nil,
	})
	if err != nil {
		m.log.Debug("retire keyboard failed (ok)", "chat_id", chatID, "msg_id", msgID, "err", err)
	}
}
