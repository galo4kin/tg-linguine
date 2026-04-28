package articles

import (
	"sync"
	"testing"
	"time"
)

func TestPendingStore_PutThenTakeReturnsItem(t *testing.T) {
	s := NewPendingStore(time.Minute)
	id := s.Put(42, Extracted{URL: "https://x", Content: "hi"})
	if id == "" {
		t.Fatalf("Put returned empty id")
	}
	got, ok := s.Take(id, 42)
	if !ok {
		t.Fatalf("Take returned not-ok for fresh id")
	}
	if got.URL != "https://x" || got.Content != "hi" {
		t.Errorf("Take returned wrong item: %+v", got)
	}
	if s.Size() != 0 {
		t.Errorf("Size after Take = %d, want 0", s.Size())
	}
}

func TestPendingStore_TakeIsOneShot(t *testing.T) {
	s := NewPendingStore(time.Minute)
	id := s.Put(1, Extracted{URL: "u"})
	if _, ok := s.Take(id, 1); !ok {
		t.Fatalf("first Take must succeed")
	}
	if _, ok := s.Take(id, 1); ok {
		t.Errorf("second Take must fail; found item under same id")
	}
}

func TestPendingStore_OtherUserCannotTake(t *testing.T) {
	s := NewPendingStore(time.Minute)
	id := s.Put(1, Extracted{URL: "u"})
	if _, ok := s.Take(id, 2); ok {
		t.Errorf("Take with wrong user must fail")
	}
	// Owner can still take it back — wrong-user lookup must not consume.
	if _, ok := s.Take(id, 1); !ok {
		t.Errorf("rightful owner must still be able to Take")
	}
}

func TestPendingStore_ExpiredEntriesAreCollected(t *testing.T) {
	s := NewPendingStore(time.Minute)
	now := time.Unix(1_000_000, 0)
	s.now = func() time.Time { return now }

	id := s.Put(1, Extracted{URL: "u"})
	if s.Size() != 1 {
		t.Fatalf("size after Put = %d, want 1", s.Size())
	}

	// Jump past TTL.
	now = now.Add(2 * time.Minute)
	if _, ok := s.Take(id, 1); ok {
		t.Errorf("expired Take must fail")
	}
	if s.Size() != 0 {
		t.Errorf("expired entry must be GC'd; size=%d", s.Size())
	}
}

func TestPendingStore_DefaultsTTLWhenZero(t *testing.T) {
	s := NewPendingStore(0)
	if s.ttl != DefaultPendingTTL {
		t.Errorf("ttl = %v, want default %v", s.ttl, DefaultPendingTTL)
	}
}

func TestPendingStore_ConcurrentPutTake(t *testing.T) {
	s := NewPendingStore(time.Minute)
	var wg sync.WaitGroup
	const n = 200
	type entry struct {
		id     string
		userID int64
	}
	entries := make(chan entry, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := s.Put(int64(i), Extracted{URL: "u"})
			entries <- entry{id: id, userID: int64(i)}
		}(i)
	}
	wg.Wait()
	close(entries)
	taken := 0
	for e := range entries {
		if _, ok := s.Take(e.id, e.userID); ok {
			taken++
		}
	}
	if taken != n {
		t.Errorf("expected to take all %d items, got %d", n, taken)
	}
}
