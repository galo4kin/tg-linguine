package telegram

import (
	"sync"
	"testing"
	"time"
)

// TestBot_Shutdown_DrainsCleanly: a held WaitGroup that releases under the
// timeout returns true.
func TestBot_Shutdown_DrainsCleanly(t *testing.T) {
	tb := &Bot{}
	tb.inflight.Add(1)
	go func() {
		time.Sleep(50 * time.Millisecond)
		tb.inflight.Done()
	}()
	if drained := tb.Shutdown(2 * time.Second); !drained {
		t.Fatalf("expected clean drain, got timeout")
	}
}

// TestBot_Shutdown_TimesOut: a permanently-held WaitGroup forces the
// timeout branch. The handler is then released to keep the test process
// from leaking goroutines.
func TestBot_Shutdown_TimesOut(t *testing.T) {
	tb := &Bot{}
	tb.inflight.Add(1)
	release := make(chan struct{})
	var once sync.Once
	go func() {
		<-release
		tb.inflight.Done()
	}()
	if drained := tb.Shutdown(50 * time.Millisecond); drained {
		t.Fatalf("expected timeout, got clean drain")
	}
	once.Do(func() { close(release) })
}
