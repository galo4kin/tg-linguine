package session_test

import (
	"reflect"
	"testing"

	"github.com/nikita/tg-linguine/internal/session"
)

func makeDeck(lemmas ...string) []session.StudyCard {
	out := make([]session.StudyCard, len(lemmas))
	for i, l := range lemmas {
		out[i] = session.StudyCard{
			DictionaryWordID: int64(i + 1),
			Lemma:            l,
		}
	}
	return out
}

func TestStudy_StartTracksDeck(t *testing.T) {
	s := session.NewStudy(0)
	s.Start(1, makeDeck("a", "b", "c"))
	snap, ok := s.Snapshot(1)
	if !ok {
		t.Fatalf("snapshot: not ok")
	}
	if len(snap.Deck) != 3 || snap.Cursor != 0 || snap.Correct != 0 || snap.Wrong != 0 {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}
	if snap.Done() {
		t.Fatalf("Done=true on fresh session")
	}
	if snap.Current().Lemma != "a" {
		t.Fatalf("expected current=a, got %q", snap.Current().Lemma)
	}
}

func TestStudy_RecordCorrectCarriesMastered(t *testing.T) {
	s := session.NewStudy(0)
	s.Start(1, makeDeck("a", "b", "c"))

	// Two non-mastered correct answers, then a mastered one.
	if _, ok := s.RecordCorrect(1, false); !ok {
		t.Fatal("rc1: not ok")
	}
	if _, ok := s.RecordCorrect(1, false); !ok {
		t.Fatal("rc2: not ok")
	}
	snap, ok := s.RecordCorrect(1, true)
	if !ok {
		t.Fatal("rc3: not ok")
	}
	if !snap.Done() {
		t.Fatalf("expected done after 3 cards, got cursor=%d", snap.Cursor)
	}
	if snap.Correct != 3 || snap.Wrong != 0 {
		t.Fatalf("counters: %+v", snap)
	}
	if !reflect.DeepEqual(snap.Mastered, []string{"c"}) {
		t.Fatalf("mastered: %v", snap.Mastered)
	}
}

func TestStudy_RecordWrongResetsStreakSemantics(t *testing.T) {
	// Streak/mastery is enforced at the DB layer (via threshold passed to
	// RecordCorrect). The FSM only tracks per-session counters, but a wrong
	// answer between two correct ones must not produce a "mastered" bump
	// when the DB never asserted one.
	s := session.NewStudy(0)
	s.Start(1, makeDeck("a", "b", "c", "d"))

	if _, ok := s.RecordCorrect(1, false); !ok {
		t.Fatal("rc1")
	}
	if _, ok := s.RecordCorrect(1, false); !ok {
		t.Fatal("rc2")
	}
	if _, ok := s.RecordWrong(1); !ok {
		t.Fatal("rw")
	}
	snap, ok := s.RecordCorrect(1, false)
	if !ok {
		t.Fatal("rc3")
	}
	if snap.Correct != 3 || snap.Wrong != 1 {
		t.Fatalf("counters: %+v", snap)
	}
	if len(snap.Mastered) != 0 {
		t.Fatalf("no card should be mastered, got %v", snap.Mastered)
	}
	if !snap.Done() {
		t.Fatalf("expected done after 4 cards, got cursor=%d", snap.Cursor)
	}
}

func TestStudy_EndDropsSession(t *testing.T) {
	s := session.NewStudy(0)
	s.Start(1, makeDeck("a", "b", "c"))
	if _, ok := s.RecordCorrect(1, true); !ok {
		t.Fatal("rc")
	}
	final, ok := s.End(1)
	if !ok {
		t.Fatal("end: not ok")
	}
	if final.Correct != 1 || len(final.Mastered) != 1 {
		t.Fatalf("final: %+v", final)
	}
	if _, ok := s.Snapshot(1); ok {
		t.Fatalf("snapshot after End must be empty")
	}
}

func TestStudy_NoSession(t *testing.T) {
	s := session.NewStudy(0)
	if _, ok := s.Snapshot(7); ok {
		t.Fatal("snapshot: ok with no session")
	}
	if _, ok := s.RecordCorrect(7, false); ok {
		t.Fatal("rc: ok with no session")
	}
	if _, ok := s.RecordWrong(7); ok {
		t.Fatal("rw: ok with no session")
	}
	if _, ok := s.End(7); ok {
		t.Fatal("end: ok with no session")
	}
}

func TestStudy_RecordsAfterDoneAreSafe(t *testing.T) {
	s := session.NewStudy(0)
	s.Start(1, makeDeck("a"))
	if _, ok := s.RecordCorrect(1, true); !ok {
		t.Fatal("rc1")
	}
	// Cursor walked off the deck; further calls must not mutate counters.
	snap, ok := s.RecordCorrect(1, true)
	if !ok {
		t.Fatal("rc2")
	}
	if snap.Correct != 1 || snap.Cursor != 1 || len(snap.Mastered) != 1 {
		t.Fatalf("idempotency broken: %+v", snap)
	}
}
