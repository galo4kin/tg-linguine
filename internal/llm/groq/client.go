package groq

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/nikita/tg-linguine/internal/llm"
)

// maxRetryAfter caps the wait we'll accept from Groq's Retry-After signal.
// Anything longer means we should bail out and tell the user to come back
// later instead of pinning a Telegram handler goroutine for minutes.
const maxRetryAfter = 60 * time.Second

// retryAfterBuffer is added to Groq's parsed Retry-After hint before
// sleeping. Groq's number sometimes underestimates by a fraction of a
// second — the precision is fine, but the rolling-minute window has not
// quite slid by the time the retry lands. A small buffer keeps a single
// retry sufficient in practice.
const retryAfterBuffer = 750 * time.Millisecond

// maxRateLimitAttempts caps the total number of attempts (initial + retries)
// per chat call. Free-tier Groq sometimes needs two retries when summarize
// and analyze land in the same TPM minute; four attempts gives us room to
// recover without ever waiting longer than maxRetryAfter cumulatively.
const maxRateLimitAttempts = 4

// retryAfterBodyRe pulls a "try again in N.NNs" hint out of Groq's 429
// JSON body. Groq encodes the precise wait time there (their docs use the
// same wording for all rate-limit messages), and it's more accurate than
// the integer-seconds Retry-After header would be.
var retryAfterBodyRe = regexp.MustCompile(`try again in (\d+(?:\.\d+)?)s`)

// parseRateLimitRetryAfter extracts a Retry-After hint from an HTTP 429
// response. It prefers the standard header (RFC 6585: delta-seconds or
// HTTP-date) and falls back to scanning the response body for Groq's
// "try again in N.NNs" wording. Returns zero when no usable hint is
// present — callers treat that as "do not retry".
func parseRateLimitRetryAfter(headers http.Header, body string) time.Duration {
	if h := strings.TrimSpace(headers.Get("Retry-After")); h != "" {
		if secs, err := strconv.ParseFloat(h, 64); err == nil && secs > 0 {
			return clampRetryAfter(time.Duration(secs * float64(time.Second)))
		}
		if t, err := http.ParseTime(h); err == nil {
			d := time.Until(t)
			if d > 0 {
				return clampRetryAfter(d)
			}
		}
	}
	if m := retryAfterBodyRe.FindStringSubmatch(body); len(m) == 2 {
		if secs, err := strconv.ParseFloat(m[1], 64); err == nil && secs > 0 {
			return clampRetryAfter(time.Duration(secs * float64(time.Second)))
		}
	}
	return 0
}

func clampRetryAfter(d time.Duration) time.Duration {
	if d > maxRetryAfter {
		return maxRetryAfter
	}
	return d
}

const defaultBaseURL = "https://api.groq.com/openai/v1"

// defaultBackoff is consulted on retryable failures (5xx and transport
// errors). The slice length is the number of retries — 2 attempts after the
// first failure, with the documented 1s, 3s waits.
var defaultBackoff = []time.Duration{1 * time.Second, 3 * time.Second}

type Client struct {
	baseURL string
	http    *http.Client
	model   string
	backoff []time.Duration
	log     *slog.Logger
}

type Option func(*Client)

func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = u }
}

func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

func WithModel(m string) Option {
	return func(c *Client) { c.model = m }
}

// WithBackoff overrides the default retry schedule. Pass zero-length sleeps
// in tests to keep them fast.
func WithBackoff(b []time.Duration) Option {
	return func(c *Client) { c.backoff = b }
}

// WithLogger lets the caller observe retry counts via a structured logger.
// Nil-safe — when unset, Client logs nothing.
func WithLogger(l *slog.Logger) Option {
	return func(c *Client) { c.log = l }
}

func New(opts ...Option) *Client {
	c := &Client{
		baseURL: defaultBaseURL,
		http:    &http.Client{Timeout: 20 * time.Second},
		backoff: defaultBackoff,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Client) ValidateAPIKey(ctx context.Context, key string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/models", nil)
	if err != nil {
		return fmt.Errorf("groq: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", llm.ErrUnavailable, err)
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return llm.ErrInvalidAPIKey
	case resp.StatusCode == http.StatusTooManyRequests:
		return llm.ErrRateLimited
	default:
		return fmt.Errorf("%w: status %d", llm.ErrUnavailable, resp.StatusCode)
	}
}

// snapshotErrorBody reads the first ~500 bytes of a non-2xx response body
// and returns it as a printable string. Used by chat / chatPlainText to put
// Groq's actual error message into log lines and wrapped errors so 4xx
// statuses (notably 413 Request Entity Too Large) are diagnosable from a
// single log line. The body is consumed; callers must not read it again.
func snapshotErrorBody(resp *http.Response) string {
	if resp == nil || resp.Body == nil {
		return ""
	}
	const max = 500
	buf := make([]byte, max)
	n, _ := io.ReadFull(resp.Body, buf)
	return strings.TrimSpace(string(buf[:n]))
}

// doWithRetry executes build() and retries on transport errors or 5xx
// responses. 4xx (auth, bad request) is returned immediately without retry —
// retrying those just burns tokens and time. Returns the last response, the
// number of retries actually performed, and any non-retryable error.
//
// build must produce a fresh request each call so request bodies (which are
// read on send) stay valid across attempts.
func (c *Client) doWithRetry(ctx context.Context, build func() (*http.Request, error)) (*http.Response, int, error) {
	var lastErr error
	for attempt := 0; attempt <= len(c.backoff); attempt++ {
		if attempt > 0 {
			wait := c.backoff[attempt-1]
			select {
			case <-ctx.Done():
				return nil, attempt - 1, ctx.Err()
			case <-time.After(wait):
			}
		}

		req, err := build()
		if err != nil {
			return nil, attempt, err
		}

		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("%w: %v", llm.ErrUnavailable, err)
			continue
		}
		if resp.StatusCode >= 500 && resp.StatusCode < 600 {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			lastErr = fmt.Errorf("%w: status %d", llm.ErrUnavailable, resp.StatusCode)
			continue
		}
		return resp, attempt, nil
	}
	return nil, len(c.backoff), lastErr
}
