// Package telegram contains the wiring layer between go-telegram/bot and the
// per-feature handlers. Rate limiting lives here because it is a transport
// concern: it gates URL submissions before they reach the per-article logic.
package telegram

import (
	"sync"
	"time"
)

// URLRateLimiter is an in-memory per-user token bucket guarding the URL
// analysis pipeline. The bucket holds at most `capacity` tokens; one token
// refills every `capacity / window`-th of `window`. With the defaults
// (capacity=10, window=1h) that is one token every 6 minutes — i.e. a steady
// long-run rate of 10 analyses per hour with a small burst headroom.
//
// The limiter is intentionally process-local (no DB persistence). The bot
// runs as a single watchdog-managed process, and the cost of a tiny burst
// surviving a restart is far smaller than the cost of writing a row per
// submission.
type URLRateLimiter struct {
	capacity int
	refill   time.Duration

	mu      sync.Mutex
	buckets map[int64]*bucket
	now     func() time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewURLRateLimiter builds a limiter with the given capacity and refill
// window. A capacity of <=0 disables the limiter (Allow always returns ok).
// The limiter starts every new user with a full bucket so a brand-new user
// can submit `capacity` URLs back-to-back without being told to wait.
func NewURLRateLimiter(capacity int, window time.Duration) *URLRateLimiter {
	r := &URLRateLimiter{
		capacity: capacity,
		buckets:  make(map[int64]*bucket),
		now:      time.Now,
	}
	if capacity > 0 && window > 0 {
		r.refill = window / time.Duration(capacity)
	}
	return r
}

// Allow tries to consume one token for the given user. When the bucket is
// empty, retryAfter is the ETA until the next token refills (rounded up to
// the nearest second so callers can format it without surprise).
func (r *URLRateLimiter) Allow(userID int64) (ok bool, retryAfter time.Duration) {
	if r == nil || r.capacity <= 0 || r.refill <= 0 {
		return true, 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	b, exists := r.buckets[userID]
	if !exists {
		b = &bucket{tokens: float64(r.capacity), last: now}
		r.buckets[userID] = b
	} else {
		elapsed := now.Sub(b.last)
		if elapsed > 0 {
			gain := float64(elapsed) / float64(r.refill)
			b.tokens += gain
			if b.tokens > float64(r.capacity) {
				b.tokens = float64(r.capacity)
			}
		}
		b.last = now
	}

	if b.tokens >= 1 {
		b.tokens -= 1
		return true, 0
	}

	missing := 1.0 - b.tokens
	wait := time.Duration(missing * float64(r.refill))
	if rem := wait % time.Second; rem != 0 {
		wait += time.Second - rem
	}
	return false, wait
}

// Capacity exposes the configured per-user limit. Callers use it to render
// the "X per hour" prefix of the rate-limit message.
func (r *URLRateLimiter) Capacity() int {
	if r == nil {
		return 0
	}
	return r.capacity
}
