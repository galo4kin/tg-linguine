package articles

import "testing"

func TestNormalizeURL(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{
			in:   "https://Example.com/post/?utm_source=newsletter&utm_medium=email&id=42",
			want: "https://example.com/post?id=42",
		},
		{
			in:   "HTTP://example.COM/PATH/#section",
			want: "http://example.com/PATH",
		},
		{
			in:   "https://news.example.com/article?fbclid=abc&gclid=xyz&topic=ai",
			want: "https://news.example.com/article?topic=ai",
		},
		{
			in:   "https://example.com/?utm_campaign=spring",
			want: "https://example.com/",
		},
		{
			in:   "https://example.com/article/",
			want: "https://example.com/article",
		},
	}
	for _, c := range cases {
		got, err := NormalizeURL(c.in)
		if err != nil {
			t.Errorf("NormalizeURL(%q) err: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("NormalizeURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeURL_Invalid(t *testing.T) {
	if _, err := NormalizeURL("not a url"); err == nil {
		t.Fatalf("expected error for missing scheme")
	}
}

func TestURLHashStable(t *testing.T) {
	a := URLHash("https://example.com/post")
	b := URLHash("https://example.com/post")
	if a != b {
		t.Fatalf("hash should be deterministic")
	}
	if len(a) != 64 {
		t.Fatalf("expected 64-char hex sha256, got %d", len(a))
	}
}
