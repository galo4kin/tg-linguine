package i18n

import (
	"log/slog"

	"github.com/nicksnyder/go-i18n/v2/i18n"
)

var supportedLangs = map[string]bool{"ru": true, "en": true, "es": true}

func For(bundle *i18n.Bundle, lang string) *i18n.Localizer {
	if !supportedLangs[lang] {
		lang = "en"
	}
	return i18n.NewLocalizer(bundle, lang, "en")
}

func T(loc *i18n.Localizer, msgID string, data any) string {
	if loc == nil {
		return msgID
	}
	msg, err := loc.Localize(&i18n.LocalizeConfig{
		MessageID:    msgID,
		TemplateData: data,
	})
	if err != nil {
		slog.Warn("i18n: missing key", "msgID", msgID)
		return msgID
	}
	return msg
}
