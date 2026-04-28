package groq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/nikita/tg-linguine/internal/llm"
)

const DefaultModel = "llama-3.3-70b-versatile"

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponseFormat struct {
	Type string `json:"type"`
}

type chatRequest struct {
	Model          string             `json:"model"`
	Messages       []chatMessage      `json:"messages"`
	ResponseFormat chatResponseFormat `json:"response_format"`
	Temperature    float64            `json:"temperature,omitempty"`
	// MaxTokens caps the response size. Groq's free tier counts
	// input + reserved output toward TPM (12K total), so leaving this
	// unset makes the model reserve its default ~16K output and 413 even
	// modest requests. Always set explicitly per call site.
	MaxTokens int `json:"max_tokens,omitempty"`
}

// analyzeMaxCompletionTokens bounds the JSON analysis response. The JSON
// has summary_target + summary_native + 3 adapted versions + words list +
// safety flags; with non-trivial vocabulary this comfortably exceeds 3K
// tokens and trips schema validation when truncated. 4000 leaves room for
// a complete payload while staying under Groq's free-tier TPM cap.
const analyzeMaxCompletionTokens = 4000

type chatChoice struct {
	Message chatMessage `json:"message"`
}

type chatResponse struct {
	Choices []chatChoice `json:"choices"`
}

// Analyze sends an article to the Groq chat-completions endpoint, asking for a
// JSON object that matches the analysis schema. On the first invalid response
// it does one retry with the schema error appended to the user prompt; if the
// second attempt is also invalid, llm.ErrSchemaInvalid is returned.
func (c *Client) Analyze(ctx context.Context, key string, req llm.AnalyzeRequest) (llm.AnalyzeResponse, error) {
	model := c.model
	if model == "" {
		model = DefaultModel
	}

	userPrompt, err := llm.RenderUserPrompt(req)
	if err != nil {
		return llm.AnalyzeResponse{}, err
	}

	messages := []chatMessage{
		{Role: "system", Content: llm.SystemPrompt()},
		{Role: "user", Content: userPrompt},
	}

	raw, err := c.chat(ctx, key, model, messages)
	if err != nil {
		return llm.AnalyzeResponse{}, err
	}

	if vErr := llm.ValidateAnalysisJSON(raw); vErr != nil {
		if c.log != nil {
			snippet := string(raw)
			if len(snippet) > 500 {
				snippet = snippet[:500]
			}
			c.log.Warn("groq.analyze schema-retry",
				"reason", llm.RetryMessage(vErr),
				"first_response_snippet", snippet,
				"first_response_len", len(raw),
			)
		}
		retryMessages := append([]chatMessage(nil), messages...)
		retryMessages = append(retryMessages,
			chatMessage{Role: "assistant", Content: string(raw)},
			chatMessage{Role: "user", Content: "Your previous response failed schema validation: " + llm.RetryMessage(vErr) + ". Reply again with a single JSON object that strictly matches the schema. No prose, no markdown."},
		)
		raw, err = c.chat(ctx, key, model, retryMessages)
		if err != nil {
			return llm.AnalyzeResponse{}, err
		}
		if vErr2 := llm.ValidateAnalysisJSON(raw); vErr2 != nil {
			return llm.AnalyzeResponse{}, vErr2
		}
	}

	var out llm.AnalyzeResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return llm.AnalyzeResponse{}, fmt.Errorf("%w: %v", llm.ErrSchemaInvalid, err)
	}
	return out, nil
}

func (c *Client) chat(ctx context.Context, key, model string, messages []chatMessage) ([]byte, error) {
	body, err := json.Marshal(chatRequest{
		Model:          model,
		Messages:       messages,
		ResponseFormat: chatResponseFormat{Type: "json_object"},
		MaxTokens:      analyzeMaxCompletionTokens,
	})
	if err != nil {
		return nil, fmt.Errorf("groq: marshal request: %w", err)
	}

	return c.withRateLimitRetry(ctx, func() ([]byte, time.Duration, error) {
		return c.chatOnce(ctx, key, body)
	}, "groq.chat")
}

// chatOnce executes a single POST to /chat/completions. On a 2xx it returns
// the assistant message bytes. On a non-2xx it returns a typed error and,
// for 429s, the parsed Retry-After hint so chat() can decide whether to
// wait and retry.
func (c *Client) chatOnce(ctx context.Context, key string, body []byte) ([]byte, time.Duration, error) {
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
			c.log.Warn("groq.chat failed",
				"groq_retries", retries,
				"errors_total", 1,
				"err", err.Error(),
			)
		}
		return nil, 0, err
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody := snapshotErrorBody(resp)
		if c.log != nil {
			c.log.Warn("groq.chat non-2xx",
				"status", resp.StatusCode,
				"body", errBody,
				"errors_total", 1,
			)
		}
		switch {
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			return nil, 0, llm.ErrInvalidAPIKey
		case resp.StatusCode == http.StatusTooManyRequests:
			return nil, parseRateLimitRetryAfter(resp.Header, errBody), llm.ErrRateLimited
		default:
			return nil, 0, fmt.Errorf("%w: status %d: %s", llm.ErrUnavailable, resp.StatusCode, errBody)
		}
	}

	var parsed chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, 0, fmt.Errorf("%w: decode chat response: %v", llm.ErrUnavailable, err)
	}
	if len(parsed.Choices) == 0 {
		return nil, 0, fmt.Errorf("%w: empty choices", llm.ErrUnavailable)
	}
	if c.log != nil {
		c.log.Info("groq.chat ok", "groq_retries", retries)
	}
	return []byte(parsed.Choices[0].Message.Content), 0, nil
}
