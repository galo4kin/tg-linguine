package handlers

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/nikita/tg-linguine/internal/articles"
	"github.com/nikita/tg-linguine/internal/crypto"
	"github.com/nikita/tg-linguine/internal/dictionary"
	tgi18n "github.com/nikita/tg-linguine/internal/i18n"
	"github.com/nikita/tg-linguine/internal/llm/mock"
	"github.com/nikita/tg-linguine/internal/users"
)

// TestURLHandler_LongArticleSendsPromptInsteadOfError exercises the
// graceful long-article path end-to-end: a long body trips the per-request
// token budget, but instead of editing the status to an error string the
// handler edits it to the localized "what should I do?" prompt and emits an
// inline keyboard with two buttons (truncate / summarize).
func TestURLHandler_LongArticleSendsPromptInsteadOfError(t *testing.T) {
	captured := &recordedTelegram{}
	tg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := r.URL.Path
		if i := strings.LastIndex(method, "/"); i >= 0 {
			method = method[i+1:]
		}
		fields := map[string]any{}
		if err := r.ParseMultipartForm(32 << 20); err == nil && r.MultipartForm != nil {
			for k, v := range r.MultipartForm.Value {
				if len(v) > 0 {
					fields[k] = v[0]
				}
			}
		} else {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &fields)
		}
		captured.record(method, fields)
		w.Header().Set("Content-Type", "application/json")
		switch method {
		case "editMessageText":
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
		default:
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":100,"date":1,"chat":{"id":1234,"type":"private"}}}`))
		}
	}))
	t.Cleanup(tg.Close)

	b, err := tgbot.New("test:token", tgbot.WithSkipGetMe(), tgbot.WithServerURL(tg.URL))
	if err != nil {
		t.Fatalf("bot.New: %v", err)
	}

	db := newHandlerTestDB(t)
	cipher := newLongCipher(t)

	usersRepo := users.NewSQLiteRepository(db)
	usersSvc := users.NewService(usersRepo)
	langs := users.NewSQLiteUserLanguageRepository(db)
	keys := users.NewSQLiteAPIKeyRepository(db, cipher)

	telegramUserID := int64(424242)
	res, err := db.Exec(`INSERT INTO users (telegram_user_id, interface_language) VALUES (?, ?)`, telegramUserID, "ru")
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	userID, _ := res.LastInsertId()
	if _, err := db.Exec(`INSERT INTO user_languages (user_id, language_code, cefr_level, is_active) VALUES (?, ?, ?, 1)`, userID, "en", "B1"); err != nil {
		t.Fatalf("seed lang: %v", err)
	}
	if err := keys.Set(context.Background(), userID, users.ProviderGroq, "gsk_test"); err != nil {
		t.Fatalf("seed key: %v", err)
	}

	articleRepo := articles.NewSQLiteRepository(db)
	dictRepo := dictionary.NewSQLiteRepository(db)
	awords := dictionary.NewSQLiteArticleWordsRepository(db)
	statuses := dictionary.NewSQLiteUserWordStatusRepository(db)

	provider := &mock.Provider{}
	longBody := strings.Repeat("alpha beta gamma delta epsilon zeta eta theta\n\n", 30)

	svc := articles.NewService(articles.ServiceDeps{
		DB:        db,
		Users:     usersSvc,
		Languages: langs,
		Keys:      keys,
		Extractor: stubLongExtractor{body: longBody},
		LLM:       provider,
		Articles:  articleRepo,
		Dictionary: dictRepo,
		ArticleWords: awords,
		Statuses:    statuses,
		MaxTokens:   50, // tiny budget guarantees parking
		Log:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	bundle, err := tgi18n.NewBundle()
	if err != nil {
		t.Fatalf("bundle: %v", err)
	}

	handler := NewURL(usersSvc, langs, svc, articleRepo, awords, db, nil, bundle, slog.New(slog.NewTextHandler(io.Discard, nil)))

	update := &models.Update{
		Message: &models.Message{
			ID:   42,
			Chat: models.Chat{ID: 1234},
			From: &models.User{ID: telegramUserID, LanguageCode: "ru"},
			Text: "https://example.com/long",
		},
	}
	handler.Handle(context.Background(), b, update)

	loc := tgi18n.For(bundle, "ru")
	wantPromptPrefix := strings.Split(tgi18n.T(loc, "article.long.prompt", map[string]int{"Words": 999}), "(")[0]

	deadline := time.Now().Add(2 * time.Second)
	for {
		captured.mu.Lock()
		var lastEdit recordedCall
		var found bool
		for _, c := range captured.calls {
			if c.method == "editMessageText" {
				lastEdit = c
				found = true
			}
		}
		captured.mu.Unlock()

		if found {
			text, _ := lastEdit.body["text"].(string)
			if !strings.HasPrefix(text, wantPromptPrefix) {
				t.Fatalf("final edit text = %q, want prompt prefix %q", text, wantPromptPrefix)
			}
			markup, _ := lastEdit.body["reply_markup"].(string)
			if !strings.Contains(markup, CallbackPrefixLongArticle) {
				t.Fatalf("expected long-article callback in reply_markup, got %q", markup)
			}
			if !strings.Contains(markup, ":t") || !strings.Contains(markup, ":s") {
				t.Fatalf("expected both :t and :s variants in reply_markup, got %q", markup)
			}
			if len(provider.AnalyzeCalls) != 0 {
				t.Errorf("LLM Analyze must not be called for parked article, got %d calls", len(provider.AnalyzeCalls))
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("no editMessageText recorded; calls=%+v", captured.calls)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type stubLongExtractor struct{ body string }

func (s stubLongExtractor) Extract(ctx context.Context, url string) (articles.Extracted, error) {
	return articles.Extracted{
		URL:           url,
		NormalizedURL: url,
		URLHash:       "long-fixture-hash",
		Title:         "Long Reference Article",
		Content:       s.body,
		Lang:          "en",
	}, nil
}

func newLongCipher(t *testing.T) *crypto.AESGCM {
	t.Helper()
	key := make([]byte, crypto.KeySize)
	rand.Read(key)
	c, err := crypto.New(key)
	if err != nil {
		t.Fatalf("crypto: %v", err)
	}
	return c
}

