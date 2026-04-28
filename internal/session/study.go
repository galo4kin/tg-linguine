package session

import (
	"sync"
	"time"
)

// StudyMasteryThreshold is the number of consecutive "remember" answers
// required before a learning word is promoted to `mastered`. Matches the
// task brief in step 22 ("3 правильных подряд → mastered").
const StudyMasteryThreshold = 3

// StudyCard is one face of a study session — what the user sees on screen
// before tapping "Помню" / "Не помню". Surface form and example sentences
// come from one representative article occurrence; the lemma + IPA come
// from dictionary_words directly.
type StudyCard struct {
	DictionaryWordID  int64
	Lemma             string
	POS               string
	SurfaceForm       string
	TranscriptionIPA  string
	TranslationNative string
	ExampleTarget     string
	ExampleNative     string
}

// StudySnapshot is the read-only view of a session — used both by the
// in-progress card render and by the final summary.
type StudySnapshot struct {
	Deck     []StudyCard
	Cursor   int
	Correct  int
	Wrong    int
	Mastered []string
}

// Done reports whether the cursor has walked off the end of the deck —
// i.e. there are no more cards to render and the summary is due.
func (s StudySnapshot) Done() bool { return s.Cursor >= len(s.Deck) }

// Current returns the card at the cursor (zero value when the deck is
// exhausted).
func (s StudySnapshot) Current() StudyCard {
	if s.Done() {
		return StudyCard{}
	}
	return s.Deck[s.Cursor]
}

// Study is the in-memory FSM for `/study` sessions. Only one active session
// per user is supported — starting a new one replaces the previous deck.
type Study struct {
	mu       sync.Mutex
	sessions map[int64]*studyEntry
	ttl      time.Duration
	now      func() time.Time
}

type studyEntry struct {
	deck     []StudyCard
	cursor   int
	correct  int
	wrong    int
	mastered []string
	updated  time.Time
}

// NewStudy builds a Study FSM. ttl bounds how long an idle session is kept
// in memory; pass 0 to disable garbage collection (useful in tests).
func NewStudy(ttl time.Duration) *Study {
	return &Study{
		sessions: make(map[int64]*studyEntry),
		ttl:      ttl,
		now:      time.Now,
	}
}

// Start replaces (or creates) the user's active session with the given
// deck. The cursor and counters are reset.
func (s *Study) Start(userID int64, deck []StudyCard) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcLocked()
	s.sessions[userID] = &studyEntry{
		deck:    deck,
		updated: s.now(),
	}
}

// Snapshot returns the current state of the user's session, or ok=false
// when there is none.
func (s *Study) Snapshot(userID int64) (StudySnapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcLocked()
	e, ok := s.sessions[userID]
	if !ok {
		return StudySnapshot{}, false
	}
	return snapshotOf(e), true
}

// RecordCorrect advances the cursor and bumps the correct counter. When
// `mastered` is true, the current card's lemma is appended to the
// mastered list. Returns the post-update snapshot and ok=false when there
// is no active session.
func (s *Study) RecordCorrect(userID int64, mastered bool) (StudySnapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcLocked()
	e, ok := s.sessions[userID]
	if !ok {
		return StudySnapshot{}, false
	}
	if e.cursor >= len(e.deck) {
		return snapshotOf(e), true
	}
	if mastered {
		e.mastered = append(e.mastered, e.deck[e.cursor].Lemma)
	}
	e.correct++
	e.cursor++
	e.updated = s.now()
	return snapshotOf(e), true
}

// RecordWrong advances the cursor and bumps the wrong counter.
func (s *Study) RecordWrong(userID int64) (StudySnapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcLocked()
	e, ok := s.sessions[userID]
	if !ok {
		return StudySnapshot{}, false
	}
	if e.cursor >= len(e.deck) {
		return snapshotOf(e), true
	}
	e.wrong++
	e.cursor++
	e.updated = s.now()
	return snapshotOf(e), true
}

// End drops the user's session and returns its final snapshot.
func (s *Study) End(userID int64) (StudySnapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.sessions[userID]
	if !ok {
		return StudySnapshot{}, false
	}
	delete(s.sessions, userID)
	return snapshotOf(e), true
}

func (s *Study) gcLocked() {
	if s.ttl <= 0 {
		return
	}
	cutoff := s.now().Add(-s.ttl)
	for id, e := range s.sessions {
		if e.updated.Before(cutoff) {
			delete(s.sessions, id)
		}
	}
}

func snapshotOf(e *studyEntry) StudySnapshot {
	deck := make([]StudyCard, len(e.deck))
	copy(deck, e.deck)
	mastered := make([]string, len(e.mastered))
	copy(mastered, e.mastered)
	return StudySnapshot{
		Deck:     deck,
		Cursor:   e.cursor,
		Correct:  e.correct,
		Wrong:    e.wrong,
		Mastered: mastered,
	}
}
