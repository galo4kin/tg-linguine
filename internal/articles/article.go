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
		if !IsCEFR(k) || v == "" {
			continue
		}
		out[k] = v
	}
	return out
}

// CEFRLevels is the canonical ordering used to translate relative
// {lower, current, higher} levels into absolute CEFR codes and back.
var CEFRLevels = []string{"A1", "A2", "B1", "B2", "C1", "C2"}

// IsCEFR reports whether s is a known CEFR code.
func IsCEFR(s string) bool {
	for _, l := range CEFRLevels {
		if l == s {
			return true
		}
	}
	return false
}

// CEFRShift returns the level `delta` steps above (positive) or below
// (negative) `base`. ok is false when `base` is unknown or the shift falls
// off the A1..C2 range.
func CEFRShift(base string, delta int) (string, bool) {
	for i, l := range CEFRLevels {
		if l != base {
			continue
		}
		j := i + delta
		if j < 0 || j >= len(CEFRLevels) {
			return "", false
		}
		return CEFRLevels[j], true
	}
	return "", false
}
