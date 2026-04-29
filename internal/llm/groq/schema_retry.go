package groq

import (
	"context"

	"github.com/nikita/tg-linguine/internal/llm"
)

// chatJSONWithSchemaRetry runs c.chat once, validates the response against
// the supplied schema validator, and on failure does a single retry with
// the validation reason appended to the conversation. Diagnostics for the
// first failure (reason + first 500 chars of the response) are logged at
// warn under "<label> schema-retry". Returns the raw bytes that passed
// validation, or the second-attempt validation error if both attempts
// failed schema.
//
// label is the slog message prefix (e.g. "groq.analyze", "groq.adapt").
func (c *Client) chatJSONWithSchemaRetry(ctx context.Context, key, model string, messages []chatMessage, maxTokens int, validate func([]byte) error, label string) ([]byte, error) {
	raw, err := c.chat(ctx, key, model, messages, maxTokens)
	if err != nil {
		return nil, err
	}

	vErr := validate(raw)
	if vErr == nil {
		return raw, nil
	}

	if c.log != nil {
		snippet := string(raw)
		if len(snippet) > 500 {
			snippet = snippet[:500]
		}
		c.log.Warn(label+" schema-retry",
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
	raw, err = c.chat(ctx, key, model, retryMessages, maxTokens)
	if err != nil {
		return nil, err
	}
	if vErr2 := validate(raw); vErr2 != nil {
		return nil, vErr2
	}
	return raw, nil
}
