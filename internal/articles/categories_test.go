package articles_test

import (
	"testing"

	"github.com/nikita/tg-linguine/internal/articles"
)

func TestNormalizeCategory(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Travel", "Travel"},
		{"travel", "Travel"},
		{"  TECH  ", "Tech"},
		{"Other", "Other"},
		{"", "Other"},
		{"fiction", "Other"},
		{"Politics & War", "Other"},
	}
	for _, c := range cases {
		if got := articles.NormalizeCategory(c.in); got != c.want {
			t.Errorf("NormalizeCategory(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsCategoryCode(t *testing.T) {
	if !articles.IsCategoryCode("Tech") {
		t.Errorf("expected Tech to be a valid code")
	}
	if articles.IsCategoryCode("tech") {
		t.Errorf("IsCategoryCode is case-sensitive; lowercase should fail")
	}
	if articles.IsCategoryCode("") {
		t.Errorf("empty string is not a valid code")
	}
}
