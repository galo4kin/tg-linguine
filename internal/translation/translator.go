package translation

import "context"

// Translator looks up a single word and returns its translation in the target
// native language. Returns an empty string (no error) when the word is not
// found. Implementations must be safe for concurrent use.
type Translator interface {
	Translate(ctx context.Context, word, fromLang, toLang string) (string, error)
}
