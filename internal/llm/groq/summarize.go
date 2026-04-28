package groq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nikita/tg-linguine/internal/llm"
)

// Summarize asks Groq to compress a long article down to roughly the
// target token count. The response is plain text (no JSON / no schema), in
// the article's original language. Used by the long-article pre-summary
// fallback in articles.Service.AnalyzeExtracted.
func (c *Client) Summarize(ctx context.Context, key string, req llm.SummarizeRequest) (string, error) {
	model := c.model
	if model == "" {
		model = DefaultModel
	}

	userPrompt, err := llm.RenderSummarizeUserPrompt(req)
	if err != nil {
		return "", err
	}

	messages := []chatMessage{
		{Role: "system", Content: llm.SummarizeSystemPrompt()},
		{Role: "user", Content: userPrompt},
	}

	// Cap the model's output budget at TargetTokens + slack so Groq's
	// TPM accounting (input + reserved output) does not blow past the
	// free-tier 12K ceiling. The +500 covers minor model overshoot.
	maxOut := req.TargetTokens + 500
	if maxOut <= 0 {
		maxOut = 2000
	}
	body, err := c.chatPlainText(ctx, key, model, messages, maxOut)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(body), nil
}

// chatPlainText is the prose-output sibling of chat. It omits the
// response_format=json_object hint so the model is free to return plain
// text. Retry / status mapping is identical.
func (c *Client) chatPlainText(ctx context.Context, key, model string, messages []chatMessage, maxTokens int) (string, error) {
	body, err := json.Marshal(struct {
		Model     string        `json:"model"`
		Messages  []chatMessage `json:"messages"`
		MaxTokens int           `json:"max_tokens,omitempty"`
	}{Model: model, Messages: messages, MaxTokens: maxTokens})
	if err != nil {
		return "", fmt.Errorf("groq: marshal request: %w", err)
	}

	for attempt := 0; attempt < 2; attempt++ {
		out, retryAfter, err := c.chatPlainTextOnce(ctx, key, body)
		if err == nil {
			return out, nil
		}
		if attempt == 0 && retryAfter > 0 {
			if c.log != nil {
				c.log.Info("groq.summarize rate-limit retry",
					"wait_ms", retryAfter.Milliseconds(),
				)
			}
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(retryAfter):
			}
			continue
		}
		return "", err
	}
	return "", llm.ErrRateLimited
}

func (c *Client) chatPlainTextOnce(ctx context.Context, key string, body []byte) (string, time.Duration, error) {
	resp, retries, err := c.doWithRetry(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("groq: build request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	})
	if err != nil {
		if c.log != nil {
			c.log.Warn("groq.summarize failed",
				"groq_retries", retries,
				"errors_total", 1,
				"err", err.Error(),
			)
		}
		return "", 0, err
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody := snapshotErrorBody(resp)
		if c.log != nil {
			c.log.Warn("groq.summarize non-2xx",
				"status", resp.StatusCode,
				"body", errBody,
				"errors_total", 1,
			)
		}
		switch {
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			return "", 0, llm.ErrInvalidAPIKey
		case resp.StatusCode == http.StatusTooManyRequests:
			return "", parseRateLimitRetryAfter(resp.Header, errBody), llm.ErrRateLimited
		default:
			return "", 0, fmt.Errorf("%w: status %d: %s", llm.ErrUnavailable, resp.StatusCode, errBody)
		}
	}

	var parsed chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", 0, fmt.Errorf("%w: decode chat response: %v", llm.ErrUnavailable, err)
	}
	if len(parsed.Choices) == 0 {
		return "", 0, fmt.Errorf("%w: empty choices", llm.ErrUnavailable)
	}
	if c.log != nil {
		c.log.Info("groq.summarize ok", "groq_retries", retries)
	}
	return parsed.Choices[0].Message.Content, 0, nil
}
