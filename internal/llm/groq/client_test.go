package groq

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nikita/tg-linguine/internal/llm"
)

func TestValidateAPIKey_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good" {
			t.Fatalf("missing/incorrect auth header: %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	if err := c.ValidateAPIKey(context.Background(), "good"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestValidateAPIKey_Invalid(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	err := c.ValidateAPIKey(context.Background(), "bad")
	if !errors.Is(err, llm.ErrInvalidAPIKey) {
		t.Fatalf("expected ErrInvalidAPIKey, got %v", err)
	}
}

func TestValidateAPIKey_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	err := c.ValidateAPIKey(context.Background(), "any")
	if !errors.Is(err, llm.ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}
}

// TestAnalyze_RetriesOn5xxThenSucceeds covers the retry path: two 503s in a
// row, then a valid response. With WithBackoff([0,0]) the test stays fast.
func TestAnalyze_RetriesOn5xxThenSucceeds(t *testing.T) {
	valid := mustFixture(t, "valid.json")
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		io.WriteString(w, chatBody(valid))
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL), WithBackoff([]time.Duration{0, 0}))
	if _, err := c.Analyze(context.Background(), "k", llm.AnalyzeRequest{TargetLanguage: "en", NativeLanguage: "ru", CEFR: "B1"}); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls (1 + 2 retries), got %d", calls)
	}
}

// TestAnalyze_RetriesExhausted: three 503s in a row → ErrUnavailable, no
// further attempts beyond the 2-retry cap.
func TestAnalyze_RetriesExhausted(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL), WithBackoff([]time.Duration{0, 0}))
	_, err := c.Analyze(context.Background(), "k", llm.AnalyzeRequest{TargetLanguage: "en", NativeLanguage: "ru", CEFR: "B1"})
	if !errors.Is(err, llm.ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable after exhausted retries, got %v", err)
	}
	if calls != 3 {
		t.Fatalf("expected exactly 3 attempts, got %d", calls)
	}
}

// TestWithRateLimitRetry_SucceedsAfterRetry verifies the shared rate-limit
// loop sleeps once on a 429 hint, then returns the second-attempt success
// payload.
func TestWithRateLimitRetry_SucceedsAfterRetry(t *testing.T) {
	t.Parallel()
	c := New()
	calls := 0
	out, err := c.withRateLimitRetry(context.Background(), func() ([]byte, time.Duration, error) {
		calls++
		if calls == 1 {
			return nil, 1 * time.Millisecond, llm.ErrRateLimited
		}
		return []byte("ok"), 0, nil
	}, "test")
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	if string(out) != "ok" {
		t.Fatalf("expected payload %q, got %q", "ok", string(out))
	}
	if calls != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
}

// TestWithRateLimitRetry_ExhaustsAttempts verifies the loop returns
// llm.ErrRateLimited once maxRateLimitAttempts is reached, regardless of
// the per-attempt error reported by once().
func TestWithRateLimitRetry_ExhaustsAttempts(t *testing.T) {
	t.Parallel()
	c := New()
	calls := 0
	_, err := c.withRateLimitRetry(context.Background(), func() ([]byte, time.Duration, error) {
		calls++
		return nil, 1 * time.Millisecond, llm.ErrRateLimited
	}, "test")
	if !errors.Is(err, llm.ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}
	if calls != maxRateLimitAttempts {
		t.Fatalf("expected %d calls, got %d", maxRateLimitAttempts, calls)
	}
}

// TestWithRateLimitRetry_NonRateLimitErrShortCircuits verifies a non-429
// error is surfaced immediately without consuming further attempts.
func TestWithRateLimitRetry_NonRateLimitErrShortCircuits(t *testing.T) {
	c := New()
	calls := 0
	want := errors.New("boom")
	_, err := c.withRateLimitRetry(context.Background(), func() ([]byte, time.Duration, error) {
		calls++
		return nil, 0, want
	}, "test")
	if !errors.Is(err, want) {
		t.Fatalf("expected boom, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected exactly 1 call, got %d", calls)
	}
}

// TestAnalyze_NoRetryOn4xx: a 401 must NOT be retried — that just burns
// requests when the user's key is wrong.
func TestAnalyze_NoRetryOn4xx(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL), WithBackoff([]time.Duration{0, 0}))
	_, err := c.Analyze(context.Background(), "k", llm.AnalyzeRequest{TargetLanguage: "en", NativeLanguage: "ru", CEFR: "B1"})
	if !errors.Is(err, llm.ErrInvalidAPIKey) {
		t.Fatalf("expected ErrInvalidAPIKey, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("4xx must not be retried, got %d calls", calls)
	}
}
