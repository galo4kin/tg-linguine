package llm

import (
	"context"
	"errors"
)

var (
	ErrInvalidAPIKey = errors.New("llm: invalid api key")
	ErrRateLimited   = errors.New("llm: rate limited")
	ErrUnavailable   = errors.New("llm: provider unavailable")
)

type Provider interface {
	ValidateAPIKey(ctx context.Context, key string) error
	Analyze(ctx context.Context, key string, req AnalyzeRequest) (AnalyzeResponse, error)
	// Adapt rewrites a single article body at a specific CEFR level. Used by
	// the regen-on-level-change path (step 19) to fill in adaptations missing
	// from a previously analyzed article without rerunning the full pipeline.
	Adapt(ctx context.Context, key string, req AdaptRequest) (AdaptResponse, error)
}
