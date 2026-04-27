package articles

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestReadabilityExtractor_Extract_Golden(t *testing.T) {
	html, err := os.ReadFile("testdata/sample_article.html")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(html)
	}))
	defer srv.Close()

	ext := NewReadabilityExtractor(5*time.Second, 256<<10)
	a, err := ext.Extract(context.Background(), srv.URL+"/post?utm_source=test&id=1")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !strings.Contains(a.Title, "Slow Reading") {
		t.Errorf("title missing keyword: %q", a.Title)
	}
	if !strings.Contains(a.Content, "comprehension") {
		t.Errorf("content missing expected text, got: %q", a.Content)
	}
	if strings.Contains(a.Content, "Buy our merch") {
		t.Errorf("content should not include navigation/ads, got: %q", a.Content)
	}
	if !strings.Contains(a.NormalizedURL, "id=1") {
		t.Errorf("normalized URL must keep id, got %q", a.NormalizedURL)
	}
	if strings.Contains(a.NormalizedURL, "utm_source") {
		t.Errorf("normalized URL must drop utm_*, got %q", a.NormalizedURL)
	}
	if a.URLHash == "" {
		t.Errorf("URLHash must be set")
	}
}

func TestReadabilityExtractor_TooLarge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		// Stream a huge body — make it exceed the limit deterministically.
		filler := strings.Repeat("a", 64<<10)
		for i := 0; i < 16; i++ {
			w.Write([]byte(filler))
		}
	}))
	defer srv.Close()

	// 100KB cap, but we send ~1MB.
	ext := NewReadabilityExtractor(5*time.Second, 100<<10)
	_, err := ext.Extract(context.Background(), srv.URL+"/big")
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("expected ErrTooLarge, got %v", err)
	}
}

func TestReadabilityExtractor_NetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ext := NewReadabilityExtractor(5*time.Second, 256<<10)
	_, err := ext.Extract(context.Background(), srv.URL+"/oops")
	if !errors.Is(err, ErrNetwork) {
		t.Fatalf("expected ErrNetwork, got %v", err)
	}
}

func TestReadabilityExtractor_NotArticle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><body></body></html>`))
	}))
	defer srv.Close()

	ext := NewReadabilityExtractor(5*time.Second, 256<<10)
	_, err := ext.Extract(context.Background(), srv.URL+"/empty")
	if !errors.Is(err, ErrNotArticle) {
		t.Fatalf("expected ErrNotArticle, got %v", err)
	}
}
