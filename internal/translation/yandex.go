package translation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const yandexDictBaseURL = "https://dictionary.yandex.net/api/v1/dicservice.json/lookup"

// YandexResponse is the top-level JSON structure returned by the Yandex
// Dictionary API. Fields are exported so tests can construct mock responses.
type YandexResponse struct {
	Def []YandexDef `json:"def"`
}

// YandexDef is one dictionary entry (one part-of-speech group).
type YandexDef struct {
	Tr []YandexTr `json:"tr"`
}

// YandexTr is one translation variant inside a definition.
type YandexTr struct {
	Text string `json:"text"`
}

// YandexClient calls the Yandex Dictionary REST API.
type YandexClient struct {
	apiKey  string
	baseURL string
	http    *http.Client
}

type YandexOption func(*YandexClient)

// WithBaseURL overrides the API endpoint — used in tests to point at a local
// httptest server.
func WithBaseURL(u string) YandexOption {
	return func(c *YandexClient) { c.baseURL = u }
}

// NewYandex creates a Yandex Dictionary client. apiKey must be non-empty.
func NewYandex(apiKey string, opts ...YandexOption) *YandexClient {
	if apiKey == "" {
		panic("yandex dict: apiKey must not be empty")
	}
	c := &YandexClient{
		apiKey:  apiKey,
		baseURL: yandexDictBaseURL,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Translate returns the top (up to 3) translations of word from fromLang to
// toLang joined by ", ". Returns "" when the word is not in the dictionary.
func (c *YandexClient) Translate(ctx context.Context, word, fromLang, toLang string) (string, error) {
	params := url.Values{
		"key":  {c.apiKey},
		"lang": {fromLang + "-" + toLang},
		"text": {word},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"?"+params.Encode(), nil)
	if err != nil {
		return "", fmt.Errorf("yandex dict: build request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("yandex dict: http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("yandex dict: status %d", resp.StatusCode)
	}

	var result YandexResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("yandex dict: decode: %w", err)
	}

	// Use first POS group only; other groups are less common and ignored.
	if len(result.Def) == 0 || len(result.Def[0].Tr) == 0 {
		return "", nil
	}

	trs := result.Def[0].Tr
	if len(trs) > 3 {
		trs = trs[:3]
	}
	parts := make([]string, len(trs))
	for i, t := range trs {
		parts[i] = t.Text
	}
	return strings.Join(parts, ", "), nil
}
