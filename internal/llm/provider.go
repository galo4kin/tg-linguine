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
	// ExtractVocab extracts vocabulary words from a text chunk without
	// generating summaries or adapted versions. Used by the long-article
	// vocab pass to scan portions of the full article that the main
	// Analyze call did not see.
	ExtractVocab(ctx context.Context, key string, req ExtractVocabRequest) (ExtractVocabResponse, error)
	// Summarize compresses a long article down to roughly TargetTokens
	// tokens, in the original (target) language. Used by the long-article
	// pre-summary fallback so the analysis pipeline can still extract
	// vocabulary and adapted versions from very long inputs without
	// dropping content wholesale (the truncate fallback is the alternative).
	Summarize(ctx context.Context, key string, req SummarizeRequest) (string, error)
}
