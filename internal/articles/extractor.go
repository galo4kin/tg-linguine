package articles

import "context"

// Extracted is the in-memory result of fetching and cleaning an article. It
// is the input to the LLM analysis step; the persisted entity lives in
// Article.
type Extracted struct {
	URL           string
	NormalizedURL string
	URLHash       string
	Title         string
	Content       string
	Lang          string
}

type Extractor interface {
	Extract(ctx context.Context, rawURL string) (Extracted, error)
}
