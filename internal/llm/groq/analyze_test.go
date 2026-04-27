package groq

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/nikita/tg-linguine/internal/llm"
)

func mustFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return string(b)
}

func chatBody(content string) string {
	b, _ := json.Marshal(chatResponse{Choices: []chatChoice{{Message: chatMessage{Role: "assistant", Content: content}}}})
	return string(b)
}

func TestAnalyze_OK(t *testing.T) {
	valid := mustFixture(t, "valid.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method: %s", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer k" {
			t.Fatalf("auth: %s", r.Header.Get("Authorization"))
		}
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode req: %v", err)
		}
		if req.Model != "test-model" {
			t.Fatalf("model: %s", req.Model)
		}
		if req.ResponseFormat.Type != "json_object" {
			t.Fatalf("response_format: %+v", req.ResponseFormat)
		}
		io.WriteString(w, chatBody(valid))
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL), WithModel("test-model"))
	got, err := c.Analyze(context.Background(), "k", llm.AnalyzeRequest{
		TargetLanguage: "en", NativeLanguage: "ru", CEFR: "B1",
		ArticleTitle: "t", ArticleText: "b",
	})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got.CEFRDetected != "B1" || len(got.Words) != 1 {
		t.Fatalf("unexpected response: %+v", got)
	}
}

func TestAnalyze_RetryThenOK(t *testing.T) {
	valid := mustFixture(t, "valid.json")
	bad := mustFixture(t, "missing_field.json")
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			io.WriteString(w, chatBody(bad))
			return
		}
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		// On retry the prompt must include the prior assistant turn and a corrective user turn.
		if len(req.Messages) < 4 {
			t.Fatalf("expected retry to carry full history, got %d msgs", len(req.Messages))
		}
		io.WriteString(w, chatBody(valid))
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	if _, err := c.Analyze(context.Background(), "k", llm.AnalyzeRequest{TargetLanguage: "en", NativeLanguage: "ru", CEFR: "B1"}); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
}

func TestAnalyze_RetryStillInvalid(t *testing.T) {
	bad := mustFixture(t, "missing_field.json")
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		io.WriteString(w, chatBody(bad))
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	_, err := c.Analyze(context.Background(), "k", llm.AnalyzeRequest{TargetLanguage: "en", NativeLanguage: "ru", CEFR: "B1"})
	if !errors.Is(err, llm.ErrSchemaInvalid) {
		t.Fatalf("expected ErrSchemaInvalid, got %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
}

func TestAnalyze_InvalidKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	_, err := c.Analyze(context.Background(), "bad", llm.AnalyzeRequest{TargetLanguage: "en", NativeLanguage: "ru", CEFR: "B1"})
	if !errors.Is(err, llm.ErrInvalidAPIKey) {
		t.Fatalf("expected ErrInvalidAPIKey, got %v", err)
	}
}

func TestAnalyze_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	_, err := c.Analyze(context.Background(), "k", llm.AnalyzeRequest{TargetLanguage: "en", NativeLanguage: "ru", CEFR: "B1"})
	if !errors.Is(err, llm.ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}
}
