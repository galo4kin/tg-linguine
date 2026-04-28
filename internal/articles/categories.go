package articles

import "strings"

// CategoryCodes is the closed set of category labels the LLM is asked to
// pick from. Anything else (typos, novel categories, empty strings) is
// normalized to "Other" — see NormalizeCategory. The set must stay in
// sync with the seed in migrations/0006_seed_categories.up.sql.
var CategoryCodes = []string{
	"Travel", "Tech", "Politics", "Sports", "Health",
	"Culture", "Business", "Science", "Other",
}

// CategoryOther is the canonical fallback when the LLM returns something
// that isn't a recognized code.
const CategoryOther = "Other"

// IsCategoryCode reports whether s is a valid (case-sensitive) category.
func IsCategoryCode(s string) bool {
	for _, c := range CategoryCodes {
		if c == s {
			return true
		}
	}
	return false
}

// NormalizeCategory maps a raw LLM-supplied category onto one of
// CategoryCodes. The match is case-insensitive (the LLM occasionally
// returns "tech" instead of "Tech"); anything we don't recognize collapses
// to "Other" so we always have a category to filter on.
func NormalizeCategory(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return CategoryOther
	}
	lower := strings.ToLower(trimmed)
	for _, c := range CategoryCodes {
		if strings.ToLower(c) == lower {
			return c
		}
	}
	return CategoryOther
}
