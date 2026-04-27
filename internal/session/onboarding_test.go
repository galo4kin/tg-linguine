package session

import (
	"testing"
	"time"
)

func TestOnboarding_Flow(t *testing.T) {
	o := NewOnboarding(time.Hour)

	if _, ok := o.Snapshot(1); ok {
		t.Fatalf("expected no entry initially")
	}

	o.Start(1)
	snap, ok := o.Snapshot(1)
	if !ok || snap.State != StateAwaitingLanguage {
		t.Fatalf("after Start expected StateAwaitingLanguage, got %v ok=%v", snap.State, ok)
	}

	o.SetLanguage(1, "ru")
	snap, _ = o.Snapshot(1)
	if snap.State != StateAwaitingLevel || snap.Language != "ru" {
		t.Fatalf("after SetLanguage expected StateAwaitingLevel/ru, got %v/%q", snap.State, snap.Language)
	}

	o.SetLevel(1, "b1")
	snap, _ = o.Snapshot(1)
	if snap.State != StateDone || snap.Level != "b1" {
		t.Fatalf("after SetLevel expected StateDone/b1, got %v/%q", snap.State, snap.Level)
	}

	o.Clear(1)
	if _, ok := o.Snapshot(1); ok {
		t.Fatalf("expected entry cleared")
	}
}

func TestOnboarding_TTLEviction(t *testing.T) {
	o := NewOnboarding(10 * time.Minute)
	now := time.Unix(0, 0)
	o.now = func() time.Time { return now }

	o.Start(1)
	if _, ok := o.Snapshot(1); !ok {
		t.Fatalf("expected entry present")
	}

	now = now.Add(11 * time.Minute)
	if _, ok := o.Snapshot(1); ok {
		t.Fatalf("expected entry to be evicted after TTL")
	}
}
