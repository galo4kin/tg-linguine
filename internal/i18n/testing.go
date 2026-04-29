package i18n

import "github.com/nicksnyder/go-i18n/v2/i18n"

// NewTestBundle loads the embedded locale files and returns a bundle
// suitable for use in unit tests. It panics on error so tests fail loudly.
func NewTestBundle() *i18n.Bundle {
	b, err := NewBundle()
	if err != nil {
		panic("i18n.NewTestBundle: " + err.Error())
	}
	return b
}
