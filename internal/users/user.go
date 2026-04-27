package users

import "time"

type User struct {
	ID                int64
	TelegramUserID    int64
	TelegramUsername  string
	FirstName         string
	InterfaceLanguage string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type TelegramUser struct {
	ID           int64
	Username     string
	FirstName    string
	LanguageCode string
}
