package telegram

import (
	"testing"

	"github.com/nikita/tg-linguine/internal/config"
)

func TestIsAdmin(t *testing.T) {
	cases := []struct {
		name string
		cfg  *config.Config
		uid  int64
		want bool
	}{
		{"nil cfg", nil, 1995215, false},
		{"unset admin", &config.Config{AdminUserID: 0}, 1995215, false},
		{"unset admin, zero user", &config.Config{AdminUserID: 0}, 0, false},
		{"admin match", &config.Config{AdminUserID: 1995215}, 1995215, true},
		{"admin mismatch", &config.Config{AdminUserID: 1995215}, 42, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsAdmin(c.cfg, c.uid); got != c.want {
				t.Errorf("IsAdmin = %v, want %v", got, c.want)
			}
		})
	}
}
