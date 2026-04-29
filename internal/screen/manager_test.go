package screen_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/nikita/tg-linguine/internal/screen"
)

// fakeStore is an in-memory Store for a single chat.
type fakeStore struct {
	msgID    int
	screenID string
	ctxJSON  string
	found    bool
}

func (s *fakeStore) Set(_ context.Context, _ int64, msgID int, sid, ctxJSON string) error {
	s.msgID, s.screenID, s.ctxJSON, s.found = msgID, sid, ctxJSON, true
	return nil
}

func (s *fakeStore) Get(_ context.Context, _ int64) (int, string, string, bool, error) {
	return s.msgID, s.screenID, s.ctxJSON, s.found, nil
}

func (s *fakeStore) Clear(_ context.Context, _ int64) error {
	s.found = false
	return nil
}

// fakeBot records calls to Sender methods.
type fakeBot struct {
	sent    []bot.SendMessageParams
	edited  []bot.EditMessageTextParams
	retired []bot.EditMessageReplyMarkupParams
	deleted []bot.DeleteMessageParams
	// nextMsgID is auto-incremented for each SendMessage call.
	nextMsgID int
}

func (f *fakeBot) SendMessage(_ context.Context, p *bot.SendMessageParams) (*models.Message, error) {
	f.sent = append(f.sent, *p)
	f.nextMsgID++
	return &models.Message{ID: f.nextMsgID}, nil
}

func (f *fakeBot) EditMessageText(_ context.Context, p *bot.EditMessageTextParams) (*models.Message, error) {
	f.edited = append(f.edited, *p)
	return &models.Message{ID: p.MessageID}, nil
}

func (f *fakeBot) EditMessageReplyMarkup(_ context.Context, p *bot.EditMessageReplyMarkupParams) (*models.Message, error) {
	f.retired = append(f.retired, *p)
	return nil, nil
}

func (f *fakeBot) DeleteMessage(_ context.Context, p *bot.DeleteMessageParams) (bool, error) {
	f.deleted = append(f.deleted, *p)
	return true, nil
}

func newMgr() (*screen.Manager, *fakeStore, *fakeBot) {
	s := &fakeStore{}
	b := &fakeBot{}
	m := screen.NewManager(s, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return m, s, b
}

func TestShow_NoActive_SendsNew(t *testing.T) {
	m, store, b := newMgr()
	ctx := context.Background()
	err := m.Show(ctx, b, 100, screen.Screen{ID: screen.ScreenWelcome, Text: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.sent) != 1 {
		t.Fatalf("expected 1 send, got %d", len(b.sent))
	}
	if !store.found || store.screenID != string(screen.ScreenWelcome) {
		t.Fatalf("store not updated: found=%v screenID=%q", store.found, store.screenID)
	}
}

func TestShow_SameID_EditsActive(t *testing.T) {
	m, store, b := newMgr()
	ctx := context.Background()
	_ = m.Show(ctx, b, 100, screen.Screen{ID: screen.ScreenSettings, Text: "v1"})
	_ = m.Show(ctx, b, 100, screen.Screen{ID: screen.ScreenSettings, Text: "v2"})
	if len(b.sent) != 1 {
		t.Fatalf("expected 1 send, got %d", len(b.sent))
	}
	if len(b.edited) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(b.edited))
	}
	if store.screenID != string(screen.ScreenSettings) {
		t.Fatal("store not updated correctly")
	}
}

func TestShow_DifferentID_RetiresAndSends(t *testing.T) {
	m, _, b := newMgr()
	ctx := context.Background()
	_ = m.Show(ctx, b, 100, screen.Screen{ID: screen.ScreenWelcome})
	_ = m.Show(ctx, b, 100, screen.Screen{ID: screen.ScreenSettings})
	if len(b.sent) != 2 {
		t.Fatalf("expected 2 sends, got %d", len(b.sent))
	}
	if len(b.retired) != 1 {
		t.Fatalf("expected 1 retire, got %d", len(b.retired))
	}
}

func TestRetireActive_StripsAndClears(t *testing.T) {
	m, store, b := newMgr()
	ctx := context.Background()
	_ = m.Show(ctx, b, 100, screen.Screen{ID: screen.ScreenWelcome})
	_ = m.RetireActive(ctx, b, 100)
	if len(b.retired) != 1 {
		t.Fatalf("expected 1 retire, got %d", len(b.retired))
	}
	if store.found {
		t.Fatal("store should be cleared after RetireActive")
	}
}

func TestReplace_AlwaysSendsNew(t *testing.T) {
	m, _, b := newMgr()
	ctx := context.Background()
	_ = m.Show(ctx, b, 100, screen.Screen{ID: screen.ScreenWelcome})
	_ = m.Replace(ctx, b, 100, screen.Screen{ID: screen.ScreenWelcome, Text: "again"})
	if len(b.sent) != 2 {
		t.Fatalf("expected 2 sends, got %d", len(b.sent))
	}
	if len(b.retired) != 1 {
		t.Fatalf("expected 1 retire, got %d", len(b.retired))
	}
}
