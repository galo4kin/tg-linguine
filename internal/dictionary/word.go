package dictionary

import "time"

// DictionaryWord is a deduplicated lemma in a given language.
type DictionaryWord struct {
	ID               int64
	LanguageCode     string
	Lemma            string
	POS              string
	TranscriptionIPA string
}

// ArticleWord ties a dictionary word to a specific article occurrence,
// carrying the surface form and translated example sentences.
type ArticleWord struct {
	ArticleID         int64
	DictionaryWordID  int64
	SurfaceForm       string
	TranslationNative string
	ExampleTarget     string
	ExampleNative     string
}

// WordStatus is the per-user learning state for a dictionary word.
type WordStatus string

const (
	StatusLearning WordStatus = "learning"
	StatusKnown    WordStatus = "known"
	StatusSkipped  WordStatus = "skipped"
	StatusMastered WordStatus = "mastered"
)

// UserWordStatus is the row in user_word_status.
type UserWordStatus struct {
	UserID           int64
	DictionaryWordID int64
	Status           WordStatus
	CorrectStreak    int
	CorrectTotal     int
	WrongTotal       int
	UpdatedAt        time.Time
}
