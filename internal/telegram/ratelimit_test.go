package telegram

import (
	"testing"
	"time"
)

// fixedClock returns a controllable now() so the bucket can be advanced
// deterministically without sleeping.
func fixedClock() (*time.Time, func() time.Time) {
	t := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	now := &t
	return now, func() time.Time { return *now }
}

func TestURLRateLimiter_AllowsCapacityThenBlocks(t *testing.T) {
	cur, clk := fixedClock()
	r := NewURLRateLimiter(10, time.Hour)
	r.now = clk

	for i := 0; i < 10; i++ {
		ok, _ := r.Allow(42)
		if !ok {
			t.Fatalf("submission %d should be allowed", i+1)
		}
	}
	// 11th submission within the hour must be blocked with a sensible ETA.
	ok, retry := r.Allow(42)
	if ok {
		t.Fatalf("11th submission should be blocked")
	}
	// One token refills every 6 minutes (1 hour / 10).
	if retry <= 0 || retry > 6*time.Minute+time.Second {
		t.Fatalf("unexpected retry-after: %s", retry)
	}

	// Advancing time past one refill window must free a slot.
	*cur = cur.Add(6 * time.Minute)
	ok, _ = r.Allow(42)
	if !ok {
		t.Fatalf("after 6 min a new token should be available")
	}

	// Capacity also reflects in the public getter for the i18n message.
	if r.Capacity() != 10 {
		t.Fatalf("Capacity() = %d, want 10", r.Capacity())
	}
}

func TestURLRateLimiter_PerUserIsolation(t *testing.T) {
	_, clk := fixedClock()
	r := NewURLRateLimiter(2, time.Hour)
	r.now = clk

	for i := 0; i < 2; i++ {
		if ok, _ := r.Allow(1); !ok {
			t.Fatalf("user 1 submission %d should be allowed", i+1)
		}
	}
	if ok, _ := r.Allow(1); ok {
		t.Fatalf("user 1 third submission should be blocked")
	}
	// User 2 has its own bucket — must not be affected.
	if ok, _ := r.Allow(2); !ok {
		t.Fatalf("user 2 must have its own fresh bucket")
	}
}

func TestURLRateLimiter_RefillFullyAfterWindow(t *testing.T) {
	cur, clk := fixedClock()
	r := NewURLRateLimiter(10, time.Hour)
	r.now = clk

	for i := 0; i < 10; i++ {
		r.Allow(7)
	}
	if ok, _ := r.Allow(7); ok {
		t.Fatalf("expected block after capacity exhausted")
	}

	// One full window later, the bucket is back to full.
	*cur = cur.Add(time.Hour)
	for i := 0; i < 10; i++ {
		if ok, _ := r.Allow(7); !ok {
			t.Fatalf("after window, submission %d should be allowed", i+1)
		}
	}
	if ok, _ := r.Allow(7); ok {
		t.Fatalf("11th submission in second window should be blocked again")
	}
}

func TestURLRateLimiter_DisabledWhenCapacityZero(t *testing.T) {
	r := NewURLRateLimiter(0, time.Hour)
	for i := 0; i < 1000; i++ {
		if ok, _ := r.Allow(1); !ok {
			t.Fatalf("disabled limiter must always allow, blocked at %d", i)
		}
	}
	if r.Capacity() != 0 {
		t.Fatalf("Capacity() on disabled limiter must be 0, got %d", r.Capacity())
	}
}

func TestURLRateLimiter_NilSafe(t *testing.T) {
	var r *URLRateLimiter
	if ok, retry := r.Allow(1); !ok || retry != 0 {
		t.Fatalf("nil limiter must allow with no wait, got ok=%v retry=%s", ok, retry)
	}
	if r.Capacity() != 0 {
		t.Fatalf("nil Capacity() should be 0")
	}
}

func TestURLRateLimiter_RetryAfterRoundsUp(t *testing.T) {
	cur, clk := fixedClock()
	r := NewURLRateLimiter(10, time.Hour)
	r.now = clk

	for i := 0; i < 10; i++ {
		r.Allow(99)
	}

	// Sub-minute progress: bucket has fewer than one token; retry-after should
	// be rounded up to the nearest second so the user-facing message can
	// format minutes without truncating to zero.
	*cur = cur.Add(time.Second * 30)
	ok, retry := r.Allow(99)
	if ok {
		t.Fatalf("expected block after partial refill")
	}
	if retry <= 0 || retry%time.Second != 0 {
		t.Fatalf("retry-after must be a positive whole-second duration, got %s", retry)
	}
}
