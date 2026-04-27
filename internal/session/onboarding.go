package session

import (
	"sync"
	"time"
)

type OnboardingState int

const (
	StateNone OnboardingState = iota
	StateAwaitingLanguage
	StateAwaitingLevel
	StateDone
)

type OnboardingSnapshot struct {
	State    OnboardingState
	Language string
	Level    string
}

type onboardingEntry struct {
	state    OnboardingState
	language string
	level    string
	updated  time.Time
}

type Onboarding struct {
	mu      sync.Mutex
	entries map[int64]*onboardingEntry
	ttl     time.Duration
	now     func() time.Time
}

func NewOnboarding(ttl time.Duration) *Onboarding {
	return &Onboarding{
		entries: make(map[int64]*onboardingEntry),
		ttl:     ttl,
		now:     time.Now,
	}
}

func (o *Onboarding) Start(userID int64) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.gcLocked()
	o.entries[userID] = &onboardingEntry{
		state:   StateAwaitingLanguage,
		updated: o.now(),
	}
}

func (o *Onboarding) SetLanguage(userID int64, lang string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.gcLocked()
	e, ok := o.entries[userID]
	if !ok {
		e = &onboardingEntry{}
		o.entries[userID] = e
	}
	e.language = lang
	e.state = StateAwaitingLevel
	e.updated = o.now()
}

func (o *Onboarding) SetLevel(userID int64, level string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.gcLocked()
	e, ok := o.entries[userID]
	if !ok {
		return
	}
	e.level = level
	e.state = StateDone
	e.updated = o.now()
}

func (o *Onboarding) Snapshot(userID int64) (OnboardingSnapshot, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.gcLocked()
	e, ok := o.entries[userID]
	if !ok {
		return OnboardingSnapshot{State: StateNone}, false
	}
	return OnboardingSnapshot{
		State:    e.state,
		Language: e.language,
		Level:    e.level,
	}, true
}

func (o *Onboarding) Clear(userID int64) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.entries, userID)
}

func (o *Onboarding) gcLocked() {
	if o.ttl <= 0 {
		return
	}
	cutoff := o.now().Add(-o.ttl)
	for id, e := range o.entries {
		if e.updated.Before(cutoff) {
			delete(o.entries, id)
		}
	}
}
