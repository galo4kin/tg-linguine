package groq

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/nikita/tg-linguine/internal/llm"
)

const defaultBaseURL = "https://api.groq.com/openai/v1"

type Client struct {
	baseURL string
	http    *http.Client
}

type Option func(*Client)

func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = u }
}

func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

func New(opts ...Option) *Client {
	c := &Client{
		baseURL: defaultBaseURL,
		http:    &http.Client{Timeout: 20 * time.Second},
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
