package groq

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nikita/tg-linguine/internal/llm"
)

func (c *Client) ExtractVocab(ctx context.Context, key string, req llm.ExtractVocabRequest) (llm.ExtractVocabResponse, error) {
	model := c.model
	if model == "" {
		model = DefaultModel
	}

	userPrompt, err := llm.RenderVocabUserPrompt(req)
	if err != nil {
		return llm.ExtractVocabResponse{}, err
	}

	messages := []chatMessage{
		{Role: "system", Content: llm.VocabSystemPrompt()},
		{Role: "user", Content: userPrompt},
	}

	raw, err := c.chatJSONWithSchemaRetry(ctx, key, model, messages, VocabExtractOutputCap, llm.ValidateVocabJSON, "groq.vocab")
	if err != nil {
		return llm.ExtractVocabResponse{}, err
	}

	var out llm.ExtractVocabResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return llm.ExtractVocabResponse{}, fmt.Errorf("%w: %v", llm.ErrSchemaInvalid, err)
	}
	return out, nil
}
