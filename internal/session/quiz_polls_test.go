package session_test

import (
	"testing"
	"time"

	"github.com/nikita/tg-linguine/internal/session"
)

func TestQuizPolls_AddAndTake(t *testing.T) {
	p := session.NewQuizPolls(0)
	p.Add("poll-1", session.QuizPollEntry{
		UserID: 42, ChatID: 100, MessageID: 7,
		Card: session.QuizCard{DictionaryWordID: 9, Lemma: "house"},
	})
	got, ok := p.Take("poll-1")
	if !ok {
		t.Fatal("expected entry to be present")
	}
	if got.UserID != 42 || got.ChatID != 100 || got.MessageID != 7 || got.Card.Lemma != "house" {
		t.Fatalf("unexpected entry: %+v", got)
	}
	if _, ok := p.Take("poll-1"); ok {
		t.Fatal("Take must remove the entry; second Take should miss")
	}
}

func TestQuizPolls_TakeUnknownIsNotOk(t *testing.T) {
	p := session.NewQuizPolls(0)
	if _, ok := p.Take("nope"); ok {
		t.Fatal("unknown poll id must return ok=false")
	}
}

func TestQuizPolls_DropForUser(t *testing.T) {
	p := session.NewQuizPolls(0)
	p.Add("a", session.QuizPollEntry{UserID: 1})
	p.Add("b", session.QuizPollEntry{UserID: 2})
	p.Add("c", session.QuizPollEntry{UserID: 1})

	p.DropForUser(1)
	if _, ok := p.Take("a"); ok {
		t.Fatal("a should have been dropped")
	}
	if _, ok := p.Take("c"); ok {
		t.Fatal("c should have been dropped")
	}
	if _, ok := p.Take("b"); !ok {
		t.Fatal("b belongs to user 2 and must survive DropForUser(1)")
	}
}

func TestQuizPolls_TTLEvicts(t *testing.T) {
	p := session.NewQuizPolls(time.Millisecond)
	p.Add("p", session.QuizPollEntry{UserID: 1})
	time.Sleep(5 * time.Millisecond)
	if _, ok := p.Take("p"); ok {
		t.Fatal("entry should have been evicted by TTL")
	}
}
