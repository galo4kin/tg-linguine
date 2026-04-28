package articles

import (
	"errors"
	nurl "net/url"
	"strings"
)

// ErrBlockedSource is returned when the URL host matches the static
// blocklist. The check fires *before* any network request, so the user
// pays nothing for an obviously off-limits source.
var ErrBlockedSource = errors.New("articles: source domain is blocked")

// ErrBlockedContent is returned when the LLM flagged the article via
// `safety_flags`. The article is NOT persisted in this case — we drop it
// silently from the user's history so a flagged piece never resurfaces.
var ErrBlockedContent = errors.New("articles: content blocked by safety flags")

// Blocklist is a case-insensitive, suffix-aware host blocklist. An entry
// "example.com" blocks both "example.com" and any "*.example.com" host.
// The zero value is a usable empty list (rejects nothing).
type Blocklist struct {
	domains map[string]struct{}
}

// NewBlocklistFromText parses a newline-delimited list of domains into a
// Blocklist. Comments start with `#`, leading/trailing whitespace and a
// leading `*.` are stripped, and entries are lowercased. Empty / invalid
// lines are skipped silently — the file ships with the binary, so a typo
// is a build-time concern.
func NewBlocklistFromText(raw string) *Blocklist {
	bl := &Blocklist{domains: map[string]struct{}{}}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Strip an inline `# comment` tail if present.
		if i := strings.Index(line, "#"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		line = strings.TrimPrefix(line, "*.")
		line = strings.TrimPrefix(line, ".")
		line = strings.ToLower(line)
		if line == "" {
			continue
		}
		bl.domains[line] = struct{}{}
	}
	return bl
}

// Size reports how many distinct domains are configured. Used by callers
// to log the loaded count at boot.
func (b *Blocklist) Size() int {
	if b == nil {
		return 0
	}
	return len(b.domains)
}

// Contains reports whether the given host is blocked. Subdomain matches
// count: with "example.com" in the list, "evil.example.com" and
// "deep.evil.example.com" are both blocked. The host is lowercased and
// any trailing dot is stripped before comparison.
func (b *Blocklist) Contains(host string) bool {
	if b == nil || len(b.domains) == 0 || host == "" {
		return false
	}
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if _, ok := b.domains[host]; ok {
		return true
	}
	// Walk parent suffixes: a.b.example.com → b.example.com → example.com.
	for i := 0; i < len(host); i++ {
		if host[i] != '.' {
			continue
		}
		if _, ok := b.domains[host[i+1:]]; ok {
			return true
		}
	}
	return false
}

// MatchURL is the convenience entry point used by AnalyzeArticle: parse
// the URL, extract the host, and run Contains. A malformed URL returns
// false — the URL handler already validates with NormalizeURL upstream,
// and a parse-failure here is not a safety event.
func (b *Blocklist) MatchURL(rawURL string) bool {
	if b == nil || len(b.domains) == 0 {
		return false
	}
	u, err := nurl.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	host := u.Hostname()
	return b.Contains(host)
}
