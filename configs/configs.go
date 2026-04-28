// Package configs holds runtime configuration assets that are bundled into
// the binary at compile time. Any file in this directory that needs to be
// embedded should be exposed through a typed variable here so callers
// import a real Go symbol rather than rely on filesystem access.
package configs

import _ "embed"

// BlockedDomainsRaw is the raw text of the static blocklist consumed by
// articles.NewBlocklistFromText. The file is one domain per line, `#`
// comments allowed; see the file itself for the human-readable header.
//
//go:embed blocked_domains.txt
var BlockedDomainsRaw string
