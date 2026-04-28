package articles

import (
	"encoding/json"
	"time"
)

// Article is a stored, analyzed article belonging to a user.
type Article struct {
	ID              int64
	UserID          int64
	SourceURL       string
	SourceURLHash   string
	Title           string
	LanguageCode    string
	CEFRDetected    string
	SummaryTarget   string
	SummaryNative   string
	AdaptedVersions string // JSON blob; populated in step 18
	CategoryID      int64  // 0 = no category
	CreatedAt       time.Time
}

// AdaptedVersions mirrors the LLM's three-level adaptation of the article
// body. Domain-local copy so the Article struct stays self-contained for the
// card renderer; the LLM package owns the wire shape.
type AdaptedVersions struct {
	Lower   string `json:"lower"`
	Current string `json:"current"`
	Higher  string `json:"higher"`
}

// ParseAdaptedVersions decodes the stored JSON blob. Returns the zero value
// for empty / malformed JSON — the renderer treats absent variants as
// "this level is unavailable" rather than as an error.
func (a *Article) ParseAdaptedVersions() AdaptedVersions {
	var v AdaptedVersions
	if a == nil || a.AdaptedVersions == "" {
		return v
	}
	_ = json.Unmarshal([]byte(a.AdaptedVersions), &v)
	return v
}
