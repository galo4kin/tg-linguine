package users

import (
	"context"
	"testing"
)

type memRepo struct {
	byID    map[int64]*User
	creates int
	nextID  int64
}

func newMemRepo() *memRepo {
	return &memRepo{byID: map[int64]*User{}}
}

func (m *memRepo) ByTelegramID(ctx context.Context, tgID int64) (*User, error) {
	if u, ok := m.byID[tgID]; ok {
		return u, nil
	}
	return nil, ErrNotFound
}

func (m *memRepo) ByID(ctx context.Context, id int64) (*User, error) {
	for _, u := range m.byID {
		if u.ID == id {
			return u, nil
		}
	}
	return nil, ErrNotFound
}

func (m *memRepo) Create(ctx context.Context, u *User) error {
	m.creates++
	m.nextID++
	u.ID = m.nextID
	m.byID[u.TelegramUserID] = u
	return nil
}

func TestRegisterUser_CreatesNewUser(t *testing.T) {
	repo := newMemRepo()
	svc := NewService(repo)

	u, created, err := svc.RegisterUser(context.Background(), TelegramUser{
		ID:           42,
		Username:     "foo",
		FirstName:    "Foo",
		LanguageCode: "ru-RU",
	})
	if err != nil {
		t.Fatalf("RegisterUser: %v", err)
	}
	if !created {
		t.Fatalf("expected created=true on first call")
	}
	if u.ID == 0 {
		t.Fatalf("expected non-zero ID")
	}
	if u.InterfaceLanguage != "ru" {
		t.Fatalf("expected normalized language ru, got %q", u.InterfaceLanguage)
	}
	if repo.creates != 1 {
		t.Fatalf("expected 1 create call, got %d", repo.creates)
	}
}

func TestRegisterUser_IdempotentOnSecondCall(t *testing.T) {
	repo := newMemRepo()
	svc := NewService(repo)
	tg := TelegramUser{ID: 7, LanguageCode: "en"}

	first, created1, err := svc.RegisterUser(context.Background(), tg)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if !created1 {
		t.Fatalf("first call expected created=true")
	}

	second, created2, err := svc.RegisterUser(context.Background(), tg)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if created2 {
		t.Fatalf("second call expected created=false")
	}
	if second.ID != first.ID {
		t.Fatalf("expected same ID, got %d vs %d", first.ID, second.ID)
	}
	if repo.creates != 1 {
		t.Fatalf("expected exactly 1 create call total, got %d", repo.creates)
	}
}

func TestNormalizeLanguage(t *testing.T) {
	cases := map[string]string{
		"ru":    "ru",
		"ru-RU": "ru",
		"en":    "en",
		"en-US": "en",
		"es":    "es",
		"fr":    "en",
		"":      "en",
		"de-DE": "en",
	}
	for in, want := range cases {
		if got := NormalizeLanguage(in); got != want {
			t.Errorf("NormalizeLanguage(%q) = %q, want %q", in, got, want)
		}
	}
}
