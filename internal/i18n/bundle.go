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

var bundle *i18n.Bundle

func init() {
	bundle = i18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("yaml", yaml.Unmarshal)

	for _, f := range []string{"en.yaml", "ru.yaml", "es.yaml"} {
		if _, err := bundle.LoadMessageFileFS(localesFS, "locales/"+f); err != nil {
			panic(fmt.Sprintf("i18n: load %s: %v", f, err))
		}
	}
}
