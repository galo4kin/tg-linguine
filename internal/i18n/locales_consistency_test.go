package i18n

import (
	"embed"
	"testing"

	"gopkg.in/yaml.v3"
)

//go:embed locales/*.yaml
var testLocalesFS embed.FS

// TestLocaleKeyParity guards step 28's i18n DoD: every key present in
// `en.yaml` must also be present in `ru.yaml` and `es.yaml`. We check
// presence only (not value equality) because translations are intentionally
// different strings — but a missing key would silently fall back to English
// for one locale, which is a UX bug.
func TestLocaleKeyParity(t *testing.T) {
	en := loadLocale(t, "locales/en.yaml")
	ru := loadLocale(t, "locales/ru.yaml")
	es := loadLocale(t, "locales/es.yaml")

	for key := range en {
		if _, ok := ru[key]; !ok {
			t.Errorf("ru.yaml missing key %q", key)
		}
		if _, ok := es[key]; !ok {
			t.Errorf("es.yaml missing key %q", key)
		}
	}
	// Reverse direction too — extra keys in ru/es with no English source are
	// dead weight and almost always a copy-paste error.
	for key := range ru {
		if _, ok := en[key]; !ok {
			t.Errorf("ru.yaml has extra key %q (not in en.yaml)", key)
		}
	}
	for key := range es {
		if _, ok := en[key]; !ok {
			t.Errorf("es.yaml has extra key %q (not in en.yaml)", key)
		}
	}
}

func loadLocale(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := testLocalesFS.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	out := map[string]any{}
	if err := yaml.Unmarshal(raw, &out); err != nil {
		t.Fatalf("yaml %s: %v", path, err)
	}
	return out
}
