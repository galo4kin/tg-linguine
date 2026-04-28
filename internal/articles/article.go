package articles

import (
	"encoding/json"
	"time"

	"github.com/nikita/tg-linguine/internal/users"
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
	AdaptedVersions string // JSON blob; absolute-CEFR keyed map (see AdaptedVersions).
	CategoryID      int64  // 0 = no category
	CreatedAt       time.Time
}

// AdaptedVersions is the persisted adaptation map, keyed by absolute CEFR
// level ("A1".."C2"). The map grows lazily as the user opens an article at
// new levels — step 19 regenerates the missing levels on demand instead of
// re-running the full article pipeline.
type AdaptedVersions map[string]string

// ParseAdaptedVersions decodes the stored JSON blob. Old / malformed JSON or
// the legacy {lower,current,higher} shape returns an empty map; the renderer
// treats absent levels as "this level is unavailable" and the regen path
// fills them in on demand.
func (a *Article) ParseAdaptedVersions() AdaptedVersions {
	if a == nil || a.AdaptedVersions == "" {
		return AdaptedVersions{}
	}
	var raw map[string]string
	if err := json.Unmarshal([]byte(a.AdaptedVersions), &raw); err != nil {
		return AdaptedVersions{}
	}
	out := AdaptedVersions{}
	for k, v := range raw {
		if !users.IsCEFR(k) || v == "" {
			continue
		}
		out[k] = v
	}
	return out
}
