package session

import (
	"math/rand"
	"sync"
	"time"
)

// QuizMasteryThreshold matches StudyMasteryThreshold — three consecutive
// correct answers promote a learning word to `mastered`. The quiz mode
// reuses the same per-word `correct_streak` column, so the constant is
// duplicated for symmetry rather than reused, to keep the two sessions'
// stats logically independent in case they diverge later.
const QuizMasteryThreshold = 3

// QuizDeckSize is the default round length surfaced to the user as
// "Карточка N/10".
const QuizDeckSize = 10

// QuizDirection picks which face of the card the user sees as the prompt.
type QuizDirection string

const (
	// QuizForeignToNative — prompt is the foreign lemma + IPA, options are
	// native translations.
	QuizForeignToNative QuizDirection = "fwd"
	// QuizNativeToForeign — prompt is the native translation, options are
	// foreign lemmas.
	QuizNativeToForeign QuizDirection = "bwd"
)

// QuizUIMode picks how a single card is rendered in the chat.
type QuizUIMode string

const (
	// QuizUIInline — regular bot message with an inline keyboard of options.
	QuizUIInline QuizUIMode = "inline"
	// QuizUIPoll — native Telegram quiz poll (sendPoll type=quiz).
	QuizUIPoll QuizUIMode = "poll"
)

// QuizCard is one question in a quiz round. It carries everything the
// handler needs to render the prompt, the options, and the post-answer
// feedback (example sentences) — the session FSM stays pure and does not
// touch the database.
type QuizCard struct {
	DictionaryWordID  int64
	Lemma             string // foreign
	POS               string
	TranscriptionIPA  string
	TranslationNative string // native, what the dictionary maps the lemma to
	ExampleTarget     string
	ExampleNative     string

	Direction    QuizDirection
	UIMode       QuizUIMode
	Options      []string // exactly four, one of which is the correct answer
	CorrectIndex int      // index into Options
}

// Prompt is the text the user sees in the question — the foreign lemma in
// the foreign→native direction, the native translation in the reverse.
func (c QuizCard) Prompt() string {
	if c.Direction == QuizNativeToForeign {
		return c.TranslationNative
	}
	return c.Lemma
}

// CorrectAnswer mirrors Prompt for the answer space.
func (c QuizCard) CorrectAnswer() string {
	if c.Direction == QuizNativeToForeign {
		return c.Lemma
	}
	return c.TranslationNative
}

// QuizSnapshot is the read-only view of a session.
type QuizSnapshot struct {
	Deck     []QuizCard
	Cursor   int
	Correct  int
	Wrong    int
	Mastered []string
}

// Done reports whether the cursor has walked off the end of the deck.
func (s QuizSnapshot) Done() bool { return s.Cursor >= len(s.Deck) }

// Current returns the card at the cursor (zero value when the deck is
// exhausted).
func (s QuizSnapshot) Current() QuizCard {
	if s.Done() {
		return QuizCard{}
	}
	return s.Deck[s.Cursor]
}

// Quiz is the in-memory FSM for the quiz mode. It mirrors Study's API so
// that the handler structure can be kept symmetric.
type Quiz struct {
	mu       sync.Mutex
	sessions map[int64]*quizEntry
	ttl      time.Duration
	now      func() time.Time
}

type quizEntry struct {
	deck     []QuizCard
	cursor   int
	correct  int
	wrong    int
	mastered []string
	updated  time.Time
}

// NewQuiz builds a Quiz FSM. ttl bounds how long an idle session is kept
// in memory; pass 0 to disable garbage collection (useful in tests).
func NewQuiz(ttl time.Duration) *Quiz {
	return &Quiz{
		sessions: make(map[int64]*quizEntry),
		ttl:      ttl,
		now:      time.Now,
	}
}

// Start replaces (or creates) the user's active session with the given
// deck. The cursor and counters are reset.
func (q *Quiz) Start(userID int64, deck []QuizCard) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.gcLocked()
	q.sessions[userID] = &quizEntry{deck: deck, updated: q.now()}
}

// Snapshot returns the current state of the user's session, or ok=false
// when there is none.
func (q *Quiz) Snapshot(userID int64) (QuizSnapshot, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.gcLocked()
	e, ok := q.sessions[userID]
	if !ok {
		return QuizSnapshot{}, false
	}
	return quizSnapshotOf(e), true
}

// RecordAnswer advances the cursor and updates counters. When `mastered`
// is true, the current card's lemma is appended to the mastered list.
// Returns the post-update snapshot and ok=false when there is no active
// session or the cursor has already reached the end.
func (q *Quiz) RecordAnswer(userID int64, correct, mastered bool) (QuizSnapshot, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.gcLocked()
	e, ok := q.sessions[userID]
	if !ok {
		return QuizSnapshot{}, false
	}
	if e.cursor >= len(e.deck) {
		return quizSnapshotOf(e), true
	}
	if correct {
		e.correct++
		if mastered {
			e.mastered = append(e.mastered, e.deck[e.cursor].Lemma)
		}
	} else {
		e.wrong++
	}
	e.cursor++
	e.updated = q.now()
	return quizSnapshotOf(e), true
}

// End drops the user's session and returns its final snapshot.
func (q *Quiz) End(userID int64) (QuizSnapshot, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	e, ok := q.sessions[userID]
	if !ok {
		return QuizSnapshot{}, false
	}
	delete(q.sessions, userID)
	return quizSnapshotOf(e), true
}

func (q *Quiz) gcLocked() {
	if q.ttl <= 0 {
		return
	}
	cutoff := q.now().Add(-q.ttl)
	for id, e := range q.sessions {
		if e.updated.Before(cutoff) {
			delete(q.sessions, id)
		}
	}
}

func quizSnapshotOf(e *quizEntry) QuizSnapshot {
	deck := make([]QuizCard, len(e.deck))
	copy(deck, e.deck)
	mastered := make([]string, len(e.mastered))
	copy(mastered, e.mastered)
	return QuizSnapshot{
		Deck:     deck,
		Cursor:   e.cursor,
		Correct:  e.correct,
		Wrong:    e.wrong,
		Mastered: mastered,
	}
}

// BuildQuizOptions assembles the four-option list for one card: the
// correct answer plus the provided distractors, shuffled. Returns the
// shuffled slice and the index of the correct answer in it. If fewer than
// `want-1` distractors are supplied, the result is shorter than `want`
// and the caller should decide how to handle it (e.g. skip the card).
//
// The shuffle uses the supplied rng so callers can pin it for tests.
func BuildQuizOptions(rng *rand.Rand, correct string, distractors []string, want int) (options []string, correctIndex int) {
	if want <= 0 {
		return nil, -1
	}
	pool := make([]string, 0, want)
	pool = append(pool, correct)
	for _, d := range distractors {
		if len(pool) >= want {
			break
		}
		pool = append(pool, d)
	}
	rng.Shuffle(len(pool), func(i, j int) {
		pool[i], pool[j] = pool[j], pool[i]
	})
	for i, v := range pool {
		if v == correct {
			return pool, i
		}
	}
	// Should not happen: correct is always inserted first.
	return pool, 0
}

// PickQuizDirection returns a direction with 50/50 probability.
func PickQuizDirection(rng *rand.Rand) QuizDirection {
	if rng.Intn(2) == 0 {
		return QuizForeignToNative
	}
	return QuizNativeToForeign
}

// PickQuizUIMode returns a UI mode with 50/50 probability.
func PickQuizUIMode(rng *rand.Rand) QuizUIMode {
	if rng.Intn(2) == 0 {
		return QuizUIInline
	}
	return QuizUIPoll
}
