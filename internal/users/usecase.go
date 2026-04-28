package users

import (
	"context"
	"errors"
)

// NormalizeLanguage maps a Telegram-provided UI language hint onto one of
// SupportedInterfaceLanguages, falling back to "en" when the locale is not
// served by the bot.
func NormalizeLanguage(code string) string {
	if len(code) >= 2 {
		code = code[:2]
	}
	if IsSupportedInterfaceLanguage(code) {
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

// SetInterfaceLanguage updates the user's UI language. The code is normalized
// against the supported set; unknown values fall back to "en" (consistent
// with NormalizeLanguage on registration).
func (s *Service) SetInterfaceLanguage(ctx context.Context, id int64, lang string) error {
	return s.repo.UpdateInterfaceLanguage(ctx, id, NormalizeLanguage(lang))
}

// DeleteUser wipes the user and all data scoped to them. See
// Repository.Delete for the exact set of tables touched.
func (s *Service) DeleteUser(ctx context.Context, id int64) error {
	return s.repo.Delete(ctx, id)
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
