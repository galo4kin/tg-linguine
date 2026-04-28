package groq

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nikita/tg-linguine/internal/llm"
)

// Adapt asks Groq to rewrite the supplied source text at a specific CEFR
// level. Same one-retry-on-schema-error pattern as Analyze.
func (c *Client) Adapt(ctx context.Context, key string, req llm.AdaptRequest) (llm.AdaptResponse, error) {
	model := c.model
	if model == "" {
		model = DefaultModel
	}

	userPrompt, err := llm.RenderAdaptUserPrompt(req)
	if err != nil {
		return llm.AdaptResponse{}, err
	}

	messages := []chatMessage{
		{Role: "system", Content: llm.AdaptSystemPrompt()},
		{Role: "user", Content: userPrompt},
	}

	raw, err := c.chat(ctx, key, model, messages)
	if err != nil {
		return llm.AdaptResponse{}, err
	}

	if vErr := llm.ValidateAdaptJSON(raw); vErr != nil {
		retryMessages := append([]chatMessage(nil), messages...)
		retryMessages = append(retryMessages,
			chatMessage{Role: "assistant", Content: string(raw)},
			chatMessage{Role: "user", Content: "Your previous response failed schema validation: " + llm.RetryMessage(vErr) + ". Reply again with a single JSON object that strictly matches the schema. No prose, no markdown."},
		)
		raw, err = c.chat(ctx, key, model, retryMessages)
		if err != nil {
			return llm.AdaptResponse{}, err
		}
		if vErr2 := llm.ValidateAdaptJSON(raw); vErr2 != nil {
			return llm.AdaptResponse{}, vErr2
		}
	}

	var out llm.AdaptResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return llm.AdaptResponse{}, fmt.Errorf("%w: %v", llm.ErrSchemaInvalid, err)
	}
	return out, nil
}
