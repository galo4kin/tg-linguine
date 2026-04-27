package i18n

import (
	"embed"
	"fmt"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
	"gopkg.in/yaml.v3"
)

//go:embed locales/*.yaml
var localesFS embed.FS

func NewBundle() (*i18n.Bundle, error) {
	b := i18n.NewBundle(language.English)
	b.RegisterUnmarshalFunc("yaml", yaml.Unmarshal)

	for _, f := range []string{"en.yaml", "ru.yaml", "es.yaml"} {
		if _, err := b.LoadMessageFileFS(localesFS, "locales/"+f); err != nil {
			return nil, fmt.Errorf("i18n: load %s: %w", f, err)
		}
	}
	return b, nil
}
