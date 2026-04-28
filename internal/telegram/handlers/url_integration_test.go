package handlers

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/nikita/tg-linguine/internal/articles"
	"github.com/nikita/tg-linguine/internal/crypto"
	"github.com/nikita/tg-linguine/internal/dictionary"
	tgi18n "github.com/nikita/tg-linguine/internal/i18n"
	"github.com/nikita/tg-linguine/internal/llm"
	"github.com/nikita/tg-linguine/internal/llm/mock"
	"github.com/nikita/tg-linguine/internal/storage"
	"github.com/nikita/tg-linguine/internal/users"
)

// TestArticleErrorMessageID_AllBranches is the cheap canary that catches the
// class of regression we just hit: a new error type bubbling up through the
// URL handler and silently falling into `error.generic` because no one added
// a `case errors.Is(...)`. Every typed error from the article pipeline that
// can legitimately reach the handler must map to a specific i18n message;
// the only legitimate `error.generic` consumer is the truly-unknown error.
func TestArticleErrorMessageID_AllBranches(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"no language", articles.ErrNoActiveLanguage, "article.err.no_language"},
		{"no api key", articles.ErrNoAPIKey, "article.err.no_api_key"},
		{"network", articles.ErrNetwork, "article.err.network"},
		{"too large", articles.ErrTooLarge, "article.err.too_large"},
		{"not article", articles.ErrNotArticle, "article.err.not_article"},
		{"paywall", articles.ErrPaywall, "article.err.paywall"},
		{"blocked source", articles.ErrBlockedSource, "article.err.blocked_source"},
		{"blocked content", articles.ErrBlockedContent, "article.err.blocked_content"},
		{"no source text", articles.ErrNoSourceText, "article.err.llm_format"},
		{"llm invalid api key", llm.ErrInvalidAPIKey, "apikey.invalid"},
		{"llm rate limited", llm.ErrRateLimited, "apikey.rate_limited"},
		{"llm unavailable", llm.ErrUnavailable, "apikey.unavailable"},
		{"llm schema invalid", llm.ErrSchemaInvalid, "article.err.llm_format"},
		{"unknown bare error", errors.New("boom"), "error.generic"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := articleErrorMessageID(tc.err)
			if got != tc.want {
				t.Fatalf("articleErrorMessageID(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

// TestURLHandler_HappyPath_RendersCard exercises the full URL handler against
// a real `*bot.Bot` pointed at an httptest mock of the Telegram API. The
// extractor and LLM provider are stubbed so the test is deterministic. The
// goal is to confirm the wiring between handler → article service → renderer
// → outbound Telegram messages works end-to-end and that no error string
// leaks to the user on the happy path.
func TestURLHandler_HappyPath_RendersCard(t *testing.T) {
	env := newURLHandlerEnv(t, &mock.Provider{AnalyzeResp: cleanAnalyzeResp()})

	env.dispatch(t, "https://example.com/article")

	final := env.lastEditText(t)
	for _, errKey := range []string{
		"article.err.llm_format", "error.generic", "apikey.unavailable",
	} {
		if strings.Contains(final, env.t(errKey)) {
			t.Fatalf("happy path produced error text %q in final edit: %s", env.t(errKey), final)
		}
	}
}

// TestURLHandler_ErrorMappingRegressions runs the URL handler against a mock
// LLM that returns each error type the production groq.Client can produce,
// then asserts the user sees the localized message that articleErrorMessageID
// maps it to. This catches the kind of failure the user reported: a typed
// error the handler doesn't know about silently degrades to "error.generic".
func TestURLHandler_ErrorMappingRegressions(t *testing.T) {
	cases := []struct {
		name       string
		analyzeErr error
		wantMsgID  string
	}{
		{"schema invalid", llm.ErrSchemaInvalid, "article.err.llm_format"},
		{"upstream unavailable", llm.ErrUnavailable, "apikey.unavailable"},
		{"rate limited", llm.ErrRateLimited, "apikey.rate_limited"},
		{"invalid api key", llm.ErrInvalidAPIKey, "apikey.invalid"},
		// Canary for the original bug class: a bare error with no errors.Is
		// ancestry must still produce a user-visible message — not a panic,
		// not an empty edit. error.generic is the documented fallback.
		{"unknown bare error", errors.New("anything"), "error.generic"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newURLHandlerEnv(t, &mock.Provider{AnalyzeErr: tc.analyzeErr})
			env.dispatch(t, "https://example.com/article")

			got := env.lastEditText(t)
			want := env.t(tc.wantMsgID)
			if got != want {
				t.Fatalf("user-facing text = %q, want %q (msgID=%s)", got, want, tc.wantMsgID)
			}
		})
	}
}

// --- helpers ---

type urlHandlerEnv struct {
	bot            *tgbot.Bot
	handler        *URLHandler
	telegramUserID int64
	loc            *goi18n.Localizer
	captured       *recordedTelegram
}

type recordedTelegram struct {
	mu    sync.Mutex
	calls []recordedCall
}

type recordedCall struct {
	method string
	body   map[string]any
}

func (r *recordedTelegram) record(method string, fields map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, recordedCall{method: method, body: fields})
}

func (e *urlHandlerEnv) dispatch(t *testing.T, urlText string) {
	t.Helper()
	update := &models.Update{
		Message: &models.Message{
			ID:   42,
			Chat: models.Chat{ID: 1234},
			From: &models.User{ID: e.telegramUserID, LanguageCode: "ru"},
			Text: urlText,
		},
	}
	e.handler.Handle(context.Background(), e.bot, update)
}

// lastEditText returns the text of the most recent editMessageText call. The
// URL handler always finalizes the user-visible state by editing the status
// message either to the article card or to the localized error.
func (e *urlHandlerEnv) lastEditText(t *testing.T) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		e.captured.mu.Lock()
		var last string
		var found bool
		for _, c := range e.captured.calls {
			if c.method == "editMessageText" {
				if s, ok := c.body["text"].(string); ok {
					last = s
					found = true
				}
			}
		}
		e.captured.mu.Unlock()
		if found {
			return last
		}
		if time.Now().After(deadline) {
			t.Fatalf("no editMessageText recorded within 2s; calls=%+v", e.captured.calls)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (e *urlHandlerEnv) t(msgID string) string {
	return tgi18n.T(e.loc, msgID, nil)
}

// newURLHandlerEnv wires up the full URLHandler stack for a single test.
// Every external boundary is in-memory or stubbed: SQLite via a temp file,
// Telegram API via httptest, LLM via mock.Provider, extractor via a stub
// returning a fixed article body. The same pattern as
// internal/articles/usecase_test.go but extended to include the handler
// layer and a real *bot.Bot.
func newURLHandlerEnv(t *testing.T, provider *mock.Provider) *urlHandlerEnv {
	t.Helper()

	captured := &recordedTelegram{}
	tg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := r.URL.Path
		if i := strings.LastIndex(method, "/"); i >= 0 {
			method = method[i+1:]
		}
		// go-telegram/bot encodes everything as multipart/form-data, including
		// plain text payloads — so parse the form first and pull `text` out
		// of the multipart values rather than JSON-decoding the body.
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
			// EditMessageText returns either a Message or `true`. We hand
			// back a boolean — go-telegram/bot accepts either shape.
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
		default:
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":100,"date":1,"chat":{"id":1234,"type":"private"}}}`))
		}
	}))
	t.Cleanup(tg.Close)

	b, err := tgbot.New("test:token",
		tgbot.WithSkipGetMe(),
		tgbot.WithServerURL(tg.URL),
	)
	if err != nil {
		t.Fatalf("bot.New: %v", err)
	}

	db := newHandlerTestDB(t)
	cipher := newHandlerCipher(t)

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

	svc := articles.NewService(articles.ServiceDeps{
		DB:           db,
		Users:        usersSvc,
		Languages:    langs,
		Keys:         keys,
		Extractor:    fixtureExtractor{},
		LLM:          provider,
		Articles:     articleRepo,
		Dictionary:   dictRepo,
		ArticleWords: awords,
		Statuses:     statuses,
		Log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	bundle, err := tgi18n.NewBundle()
	if err != nil {
		t.Fatalf("bundle: %v", err)
	}

	handler := NewURL(
		usersSvc, langs, svc, articleRepo, awords, db,
		nil, // no rate limiter — tests bypass it
		bundle,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	return &urlHandlerEnv{
		bot:            b,
		handler:        handler,
		telegramUserID: telegramUserID,
		loc:            tgi18n.For(bundle, "ru"),
		captured:       captured,
	}
}

func newHandlerTestDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := storage.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.RunMigrations(db, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func newHandlerCipher(t *testing.T) *crypto.AESGCM {
	t.Helper()
	key := make([]byte, crypto.KeySize)
	rand.Read(key)
	c, err := crypto.New(key)
	if err != nil {
		t.Fatalf("crypto: %v", err)
	}
	return c
}

// fixtureExtractor returns a fixed reference article. The content is short
// enough to stay under the default token budget and long enough that the
// pipeline won't reject it for being trivial.
type fixtureExtractor struct{}

func (fixtureExtractor) Extract(ctx context.Context, url string) (articles.Extracted, error) {
	body := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 30)
	return articles.Extracted{
		URL:           url,
		NormalizedURL: url,
		URLHash:       "test-fixture-hash",
		Title:         "Reference Article",
		Content:       body,
		Lang:          "en",
	}, nil
}

// cleanAnalyzeResp is a minimal valid LLM response for the happy path.
func cleanAnalyzeResp() llm.AnalyzeResponse {
	return llm.AnalyzeResponse{
		SummaryTarget: "A short summary in target language.",
		SummaryNative: "Краткое содержание.",
		Category:      "Tech",
		CEFRDetected:  "B1",
		AdaptedVersions: llm.AdaptedVersions{
			Lower:   "Easy version of the article.",
			Current: "Current-level version of the article.",
			Higher:  "Harder version of the article.",
		},
		Words: []llm.AnalyzedWord{
			{
				SurfaceForm: "fox", Lemma: "fox", POS: "noun",
				TranscriptionIPA: "/fɒks/", TranslationNative: "лиса",
				ExampleTarget: "The fox jumps.", ExampleNative: "Лиса прыгает.",
			},
		},
		SafetyFlags: []string{},
	}
}
