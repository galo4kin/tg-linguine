package groq

import (
	"net/http"
	"testing"
	"time"
)

func TestParseRateLimitRetryAfter_HeaderSeconds(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "12")
	got := parseRateLimitRetryAfter(h, "")
	if got != 12*time.Second {
		t.Errorf("got %v, want 12s", got)
	}
}

func TestParseRateLimitRetryAfter_HeaderFractional(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "9.75")
	got := parseRateLimitRetryAfter(h, "")
	if got < 9*time.Second || got > 10*time.Second {
		t.Errorf("got %v, want ~9.75s", got)
	}
}

func TestParseRateLimitRetryAfter_BodyExtract(t *testing.T) {
	body := `{"error":{"message":"Rate limit reached ... Limit 12000, Used 9934, Requested 4016. Please try again in 9.75s. Need more tokens?"}}`
	got := parseRateLimitRetryAfter(http.Header{}, body)
	if got < 9*time.Second || got > 10*time.Second {
		t.Errorf("got %v, want ~9.75s", got)
	}
}

func TestParseRateLimitRetryAfter_BodyIntSeconds(t *testing.T) {
	body := `... try again in 30s. ...`
	got := parseRateLimitRetryAfter(http.Header{}, body)
	if got != 30*time.Second {
		t.Errorf("got %v, want 30s", got)
	}
}

func TestParseRateLimitRetryAfter_HeaderTakesPrecedence(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "5")
	body := `try again in 99s`
	got := parseRateLimitRetryAfter(h, body)
	if got != 5*time.Second {
		t.Errorf("got %v, want 5s (header should win)", got)
	}
}

func TestParseRateLimitRetryAfter_NoSignal(t *testing.T) {
	got := parseRateLimitRetryAfter(http.Header{}, "no hint here")
	if got != 0 {
		t.Errorf("got %v, want 0 for no hint", got)
	}
}

func TestParseRateLimitRetryAfter_ClampsToMax(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "3600")
	got := parseRateLimitRetryAfter(h, "")
	if got != MaxRetryAfter {
		t.Errorf("got %v, want clamp to %v", got, MaxRetryAfter)
	}
}

func TestParseRateLimitRetryAfter_HeaderHTTPDate(t *testing.T) {
	future := time.Now().Add(7 * time.Second).UTC().Format(http.TimeFormat)
	h := http.Header{}
	h.Set("Retry-After", future)
	got := parseRateLimitRetryAfter(h, "")
	if got <= 0 || got > 10*time.Second {
		t.Errorf("got %v, want ~7s", got)
	}
}
