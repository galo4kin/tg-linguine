package session

import (
	"sync"
	"time"
)

type APIKeyWaiter struct {
	mu      sync.Mutex
	waiting map[int64]time.Time
	ttl     time.Duration
	now     func() time.Time
}

func NewAPIKeyWaiter(ttl time.Duration) *APIKeyWaiter {
	return &APIKeyWaiter{
		waiting: make(map[int64]time.Time),
		ttl:     ttl,
		now:     time.Now,
	}
}

func (w *APIKeyWaiter) Arm(userID int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.gcLocked()
	w.waiting[userID] = w.now()
}

func (w *APIKeyWaiter) IsArmed(userID int64) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.gcLocked()
	_, ok := w.waiting[userID]
	return ok
}

func (w *APIKeyWaiter) Disarm(userID int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.waiting, userID)
}

func (w *APIKeyWaiter) gcLocked() {
	if w.ttl <= 0 {
		return
	}
	cutoff := w.now().Add(-w.ttl)
	for id, t := range w.waiting {
		if t.Before(cutoff) {
			delete(w.waiting, id)
		}
	}
}
