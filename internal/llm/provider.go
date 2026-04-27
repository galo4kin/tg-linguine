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
}
