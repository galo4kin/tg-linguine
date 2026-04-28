package groq

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/nikita/tg-linguine/internal/llm"
)

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
