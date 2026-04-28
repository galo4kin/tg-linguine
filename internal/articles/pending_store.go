package articles

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

// DefaultPendingTTL controls how long a too-long extracted article hangs
// around waiting for the user to choose a fallback (truncate or summarize).
// 10 minutes is enough for a thinking pause but short enough that the
// in-memory map cannot grow indefinitely.
const DefaultPendingTTL = 10 * time.Minute

// pendingItem holds an extracted article that exceeded the per-request token
// budget. It is parked in a PendingStore until the user clicks one of the
// fallback buttons (or the TTL fires).
type pendingItem struct {
	UserID    int64
	Extracted Extracted
	ExpiresAt time.Time
}

// PendingStore is the in-memory parking lot for too-long extracted articles.
// Single-instance bot — no need for Redis or DB-backed state.
type PendingStore struct {
	mu    sync.Mutex
	items map[string]pendingItem
	ttl   time.Duration
	now   func() time.Time
}

// NewPendingStore returns a store with the supplied TTL. Pass zero to use
// DefaultPendingTTL.
func NewPendingStore(ttl time.Duration) *PendingStore {
	if ttl <= 0 {
		ttl = DefaultPendingTTL
	}
	return &PendingStore{
		items: make(map[string]pendingItem),
		ttl:   ttl,
		now:   time.Now,
	}
}

// Put parks the extracted article under a fresh random id (10 url-safe
// chars) and returns that id. The caller embeds the id into callback_data
// so a subsequent click can retrieve the article via Take.
func (s *PendingStore) Put(userID int64, ex Extracted) string {
	id := newPendingID()
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcLocked(now)
	s.items[id] = pendingItem{
		UserID:    userID,
		Extracted: ex,
		ExpiresAt: now.Add(s.ttl),
	}
	return id
}

// Take removes and returns the item matching id, scoped to userID. ok=false
// when the id is unknown, expired, or owned by a different user. The caller
// is responsible for surfacing a "session expired" message in the false
// branch.
func (s *PendingStore) Take(id string, userID int64) (Extracted, bool) {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcLocked(now)
	item, ok := s.items[id]
	if !ok {
		return Extracted{}, false
	}
	if item.UserID != userID {
		return Extracted{}, false
	}
	delete(s.items, id)
	return item.Extracted, true
}

// Size reports the number of currently parked items. Test-only helper.
func (s *PendingStore) Size() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcLocked(s.now())
	return len(s.items)
}

func (s *PendingStore) gcLocked(now time.Time) {
	for id, item := range s.items {
		if now.After(item.ExpiresAt) {
			delete(s.items, id)
		}
	}
}

func newPendingID() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		// crypto/rand on darwin/linux does not fail in practice; if it
		// does, a time-based fallback keeps the bot functional.
		ts := time.Now().UnixNano()
		for i := 0; i < 8; i++ {
			raw[i] = byte(ts >> (8 * i))
		}
	}
	return base64.RawURLEncoding.EncodeToString(raw[:])
}
