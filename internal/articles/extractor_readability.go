package articles

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	nurl "net/url"
	"strings"
	"time"

	readability "github.com/go-shiori/go-readability"
)

var (
	ErrNetwork    = errors.New("articles: network error")
	ErrTooLarge   = errors.New("articles: response too large")
	ErrNotArticle = errors.New("articles: page is not an article")
	ErrPaywall    = errors.New("articles: page looks like a paywall")
)

const minArticleChars = 200

type ReadabilityExtractor struct {
	http        *http.Client
	maxBodySize int64
	userAgent   string
}

type ReadabilityOption func(*ReadabilityExtractor)

func WithHTTPClient(c *http.Client) ReadabilityOption {
	return func(e *ReadabilityExtractor) { e.http = c }
}

func WithUserAgent(ua string) ReadabilityOption {
	return func(e *ReadabilityExtractor) { e.userAgent = ua }
}

// NewReadabilityExtractor builds an extractor backed by go-readability.
// `maxBodyBytes` caps the response body to protect against multi-MB pages.
func NewReadabilityExtractor(timeout time.Duration, maxBodyBytes int64, opts ...ReadabilityOption) *ReadabilityExtractor {
	e := &ReadabilityExtractor{
		http:        &http.Client{Timeout: timeout},
		maxBodySize: maxBodyBytes,
		userAgent:   "tg-linguine/1.0 (+https://github.com/nikita/tg-linguine)",
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

func (e *ReadabilityExtractor) Extract(ctx context.Context, rawURL string) (Article, error) {
	normalized, err := NormalizeURL(rawURL)
	if err != nil {
		return Article{}, err
	}
	parsed, err := nurl.Parse(normalized)
	if err != nil {
		return Article{}, fmt.Errorf("articles: reparse: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, normalized, nil)
	if err != nil {
		return Article{}, fmt.Errorf("articles: build request: %w", err)
	}
	req.Header.Set("User-Agent", e.userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := e.http.Do(req)
	if err != nil {
		return Article{}, fmt.Errorf("%w: %v", ErrNetwork, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Article{}, fmt.Errorf("%w: status %d", ErrNetwork, resp.StatusCode)
	}

	body, err := readLimited(resp.Body, e.maxBodySize)
	if err != nil {
		return Article{}, err
	}

	article, err := readability.FromReader(strings.NewReader(string(body)), parsed)
	if err != nil {
		return Article{}, fmt.Errorf("%w: readability: %v", ErrNotArticle, err)
	}

	text := strings.TrimSpace(article.TextContent)
	if text == "" {
		return Article{}, ErrNotArticle
	}
	if len(text) < minArticleChars && looksLikePaywall(text) {
		return Article{}, ErrPaywall
	}

	return Article{
		URL:           rawURL,
		NormalizedURL: normalized,
		URLHash:       URLHash(normalized),
		Title:         strings.TrimSpace(article.Title),
		Content:       text,
		Lang:          strings.ToLower(strings.TrimSpace(article.Language)),
	}, nil
}

func readLimited(r io.Reader, max int64) ([]byte, error) {
	if max <= 0 {
		return io.ReadAll(r)
	}
	limited := io.LimitReader(r, max+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("%w: read body: %v", ErrNetwork, err)
	}
	if int64(len(body)) > max {
		return nil, ErrTooLarge
	}
	return body, nil
}

var paywallHints = []string{
	"subscribe", "subscriber", "subscription",
	"подпис",
	"suscríbete", "suscripción",
}

func looksLikePaywall(text string) bool {
	lower := strings.ToLower(text)
	for _, hint := range paywallHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}
