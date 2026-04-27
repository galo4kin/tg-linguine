package users

import (
	"context"
	"errors"
)

var supportedLanguages = map[string]bool{"ru": true, "en": true, "es": true}

func NormalizeLanguage(code string) string {
	if len(code) >= 2 {
		code = code[:2]
	}
	if supportedLanguages[code] {
		return code
	}
	return "en"
}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) ByID(ctx context.Context, id int64) (*User, error) {
	return s.repo.ByID(ctx, id)
}

func (s *Service) RegisterUser(ctx context.Context, tg TelegramUser) (*User, bool, error) {
	existing, err := s.repo.ByTelegramID(ctx, tg.ID)
	if err == nil {
		return existing, false, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, false, err
	}

	u := &User{
		TelegramUserID:    tg.ID,
		TelegramUsername:  tg.Username,
		FirstName:         tg.FirstName,
		InterfaceLanguage: NormalizeLanguage(tg.LanguageCode),
	}
	if err := s.repo.Create(ctx, u); err != nil {
		return nil, false, err
	}
	return u, true, nil
}
