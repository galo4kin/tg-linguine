package articles

import "context"

type Article struct {
	URL           string
	NormalizedURL string
	URLHash       string
	Title         string
	Content       string
	Lang          string
}

type Extractor interface {
	Extract(ctx context.Context, rawURL string) (Article, error)
}
