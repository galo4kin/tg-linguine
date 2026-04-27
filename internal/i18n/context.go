package i18n

import (
	"context"

	"github.com/nicksnyder/go-i18n/v2/i18n"
)

type contextKey struct{}

func WithLocalizer(ctx context.Context, loc *i18n.Localizer) context.Context {
	return context.WithValue(ctx, contextKey{}, loc)
}

func FromContext(ctx context.Context) *i18n.Localizer {
	if loc, ok := ctx.Value(contextKey{}).(*i18n.Localizer); ok {
		return loc
	}
	return For("en")
}
