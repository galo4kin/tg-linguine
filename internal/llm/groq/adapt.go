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

	raw, err := c.chatJSONWithSchemaRetry(ctx, key, model, messages, llm.ValidateAdaptJSON, "groq.adapt")
	if err != nil {
		return llm.AdaptResponse{}, err
	}

	var out llm.AdaptResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return llm.AdaptResponse{}, fmt.Errorf("%w: %v", llm.ErrSchemaInvalid, err)
	}
	return out, nil
}
