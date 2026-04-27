package groq

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

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
