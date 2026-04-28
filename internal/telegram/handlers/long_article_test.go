package handlers

import (
	"testing"

	"github.com/nikita/tg-linguine/internal/articles"
)

func TestParseLongArticleCallback(t *testing.T) {
	cases := []struct {
		in       string
		wantID   string
		wantMode articles.LongAnalysisMode
		wantOK   bool
	}{
		{"lng:abcd1234:t", "abcd1234", articles.ModeTruncate, true},
		{"lng:abcd1234:s", "abcd1234", articles.ModeSummarize, true},
		{"lng::t", "", 0, false},                  // empty id
		{"lng:abcd1234:x", "", 0, false},          // unknown variant
		{"lng:abcd1234", "", 0, false},            // no variant
		{"lng:abcd1234:t:extra", "", 0, false},    // extra segment
		{"art:abcd1234:t", "", 0, false},          // wrong prefix
		{"", "", 0, false},                        // empty
	}
	for _, c := range cases {
		gotID, gotMode, gotOK := parseLongArticleCallback(c.in)
		if gotID != c.wantID || gotMode != c.wantMode || gotOK != c.wantOK {
			t.Errorf("parseLongArticleCallback(%q) = (%q, %d, %v), want (%q, %d, %v)",
				c.in, gotID, gotMode, gotOK, c.wantID, c.wantMode, c.wantOK)
		}
	}
}
