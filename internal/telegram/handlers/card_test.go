package handlers

import "testing"

func TestCardCallback_RoundTrip(t *testing.T) {
	cases := []struct {
		name string
		view ArticleView
	}{
		{"current/target", ArticleView{Level: CardLevelCurrent}},
		{"lower/target", ArticleView{Level: CardLevelLower}},
		{"higher/native", ArticleView{Level: CardLevelHigher, SummaryNative: true}},
		{"current/native", ArticleView{Level: CardLevelCurrent, SummaryNative: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cb := cardCallback(42, c.view)
			// Strip the public prefix; parseCardCallback expects the suffix.
			payload := cb[len(CallbackPrefixCard):]
			id, view, ok := parseCardCallback(payload)
			if !ok {
				t.Fatalf("parseCardCallback failed for %q", cb)
			}
			if id != 42 {
				t.Fatalf("article id: got %d want 42", id)
			}
			if view != c.view {
				t.Fatalf("view round-trip: got %+v want %+v", view, c.view)
			}
		})
	}
}

func TestCardCallback_BadPayload(t *testing.T) {
	bad := []string{"", "noop", "v", "v:42", "v:abc:c:t", "x:42:c:t"}
	for _, p := range bad {
		if _, _, ok := parseCardCallback(p); ok {
			t.Fatalf("expected parseCardCallback(%q) to fail", p)
		}
	}
}
