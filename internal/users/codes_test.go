package users_test

import (
	"testing"

	"github.com/nikita/tg-linguine/internal/users"
)

func TestIsSupportedLearningLanguage(t *testing.T) {
	cases := map[string]bool{
		"en": true,
		"es": true,
		"ru": true,
		"de": false,
		"":   false,
	}
	for in, want := range cases {
		if got := users.IsSupportedLearningLanguage(in); got != want {
			t.Errorf("IsSupportedLearningLanguage(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestIsCEFR(t *testing.T) {
	cases := map[string]bool{
		"A1": true, "A2": true, "B1": true, "B2": true, "C1": true, "C2": true,
		"a1": false, // case-sensitive by design
		"":    false,
		"D1":  false,
	}
	for in, want := range cases {
		if got := users.IsCEFR(in); got != want {
			t.Errorf("IsCEFR(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestCEFRShift(t *testing.T) {
	type want struct {
		level string
		ok    bool
	}
	cases := []struct {
		base  string
		delta int
		want  want
	}{
		{"A1", -1, want{"", false}}, // off the bottom
		{"A1", 0, want{"A1", true}},
		{"A1", 1, want{"A2", true}},
		{"B1", -1, want{"A2", true}},
		{"B1", 1, want{"B2", true}},
		{"C2", 1, want{"", false}}, // off the top
		{"unknown", 0, want{"", false}},
	}
	for _, c := range cases {
		gotLvl, gotOK := users.CEFRShift(c.base, c.delta)
		if gotLvl != c.want.level || gotOK != c.want.ok {
			t.Errorf("CEFRShift(%q, %d) = (%q, %v), want (%q, %v)",
				c.base, c.delta, gotLvl, gotOK, c.want.level, c.want.ok)
		}
	}
}
