package articles

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	nurl "net/url"
	"strings"
)

var trackingParamPrefixes = []string{"utm_"}
var trackingParamExact = map[string]struct{}{
	"fbclid":   {},
	"gclid":    {},
	"yclid":    {},
	"mc_cid":   {},
	"mc_eid":   {},
	"igshid":   {},
	"_hsenc":   {},
	"_hsmi":    {},
	"vero_id":  {},
	"ref_src":  {},
}

// NormalizeURL strips tracking query params and fragments, lowercases the host,
// and removes a trailing slash from the path. Returns the canonical form.
func NormalizeURL(raw string) (string, error) {
	u, err := nurl.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("articles: parse url: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("articles: url missing scheme or host: %q", raw)
	}

	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	u.RawFragment = ""

	q := u.Query()
	for k := range q {
		if isTrackingParam(k) {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode()

	if u.Path != "/" {
		u.Path = strings.TrimRight(u.Path, "/")
	}

	return u.String(), nil
}

func isTrackingParam(name string) bool {
	lower := strings.ToLower(name)
	if _, ok := trackingParamExact[lower]; ok {
		return true
	}
	for _, p := range trackingParamPrefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}

// URLHash returns sha256(normalized) hex-encoded.
func URLHash(normalizedURL string) string {
	sum := sha256.Sum256([]byte(normalizedURL))
	return hex.EncodeToString(sum[:])
}
