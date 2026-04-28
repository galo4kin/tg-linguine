package session

import (
	"sync"
	"time"
)

// QuizPollEntry is one row in the in-memory pollID→context map. The
// `poll_answer` update from Telegram does not carry chat_id (or any
// reference to the original message), so we have to remember everything
// the feedback message will need to render.
type QuizPollEntry struct {
	UserID    int64
	ChatID    int64
	MessageID int // poll message id, kept for stopPoll if we ever want it
	Card      QuizCard
	createdAt time.Time
}

// QuizPolls maps Telegram poll IDs to the FSM context that produced them.
// It mirrors the shape of Quiz: an in-memory store with a TTL-based GC
// and an injectable clock for tests.
type QuizPolls struct {
	mu      sync.Mutex
	entries map[string]*QuizPollEntry
	ttl     time.Duration
	now     func() time.Time
}

// NewQuizPolls builds a QuizPolls registry. ttl bounds how long an
// orphaned entry survives in memory; pass 0 to disable GC (useful in
// tests).
func NewQuizPolls(ttl time.Duration) *QuizPolls {
	return &QuizPolls{
		entries: make(map[string]*QuizPollEntry),
		ttl:     ttl,
		now:     time.Now,
	}
}

// Add registers a freshly-sent quiz poll. Overwrites any prior entry for
// the same pollID (Telegram poll IDs are globally unique, so collisions
// only happen in tests with reused fixtures).
func (p *QuizPolls) Add(pollID string, e QuizPollEntry) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.gcLocked()
	e.createdAt = p.now()
	p.entries[pollID] = &e
}

// Take removes and returns the entry for pollID. Polls are single-shot
// (one answer per round), so the answer handler consumes the entry on
// success; stale repeats then fall through to ok=false.
func (p *QuizPolls) Take(pollID string) (QuizPollEntry, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.gcLocked()
	e, ok := p.entries[pollID]
	if !ok {
		return QuizPollEntry{}, false
	}
	delete(p.entries, pollID)
	return *e, true
}

// DropForUser purges every entry that belongs to userID. Used when the
// user ends the round early — leftover poll messages are still in the
// chat, but their poll_answer updates should be ignored.
func (p *QuizPolls) DropForUser(userID int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, e := range p.entries {
		if e.UserID == userID {
			delete(p.entries, id)
		}
	}
}

func (p *QuizPolls) gcLocked() {
	if p.ttl <= 0 {
		return
	}
	cutoff := p.now().Add(-p.ttl)
	for id, e := range p.entries {
		if e.createdAt.Before(cutoff) {
			delete(p.entries, id)
		}
	}
}
