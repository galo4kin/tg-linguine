package articles

import "time"

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
