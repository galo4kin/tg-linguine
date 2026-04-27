# Code review — tasks 01–05

## Scope
All code added in steps 01–05: Go skeleton, config/logging, SQLite migrations,
Telegram skeleton, i18n.

---

## Findings

### 1. `go.mod` Go version is incorrect
`go 1.26.2` — as of 2026-04 the stable release is 1.23 / 1.24.
This was set by whatever `go` binary is installed locally. Not a bug but
worth aligning with the target deploy environment.
**Severity: low** — does not affect correctness.

### 2. `i18n/bundle.go` uses `init()` with a `panic`
Loading locales in `init()` means any test or binary that imports the package
will panic if a YAML file is malformed. Better pattern: return the bundle from
an exported `NewBundle() (*i18n.Bundle, error)` and call it from `main`.
**Severity: medium** — panic is too blunt for a startup error; hard to test.

### 3. `storage/migrate.go` imports `sqlite` driver for the migrate adapter
The migrate sqlite adapter (`github.com/golang-migrate/migrate/v4/database/sqlite`)
relies on `github.com/mattn/go-sqlite3` (CGO). Currently the import path
`"github.com/golang-migrate/migrate/v4/database/sqlite"` may pull in CGO.
Should use `"github.com/golang-migrate/migrate/v4/database/sqlite3"` (the
modernc/pure-Go adapter) — or confirm the correct adapter for `modernc.org/sqlite`.
The build passes because the pure-Go driver is registered under the name `"sqlite"`
but this should be double-checked.
**Severity: medium** — risk of accidentally pulling CGO on cross-compile.

### 4. `config.Config` fields are public but carry raw secrets
`BotToken` and `EncryptionKey` are plain `string` fields — they will appear
in any `fmt.Printf("%+v", cfg)` or JSON marshal. A `Secret` wrapper type with
a `String() string` that returns `"***"` would prevent accidental leaks.
**Severity: low** — the DoD mentions secrets must not appear in logs; a
`fmt.Sprint(cfg)` still leaks them unless the type hides its value.

### 5. `internal/telegram/bot.go` middleware order
`i18nMiddleware` and `logMiddleware` are registered in that order but the
log middleware runs *after* the i18n middleware sets the context. This is fine,
but the log middleware reads `update.Message.From.LanguageCode` directly rather
than confirming the context value. Consistent but redundant — minor.
**Severity: negligible**.

### 6. `locales/` root directory is now empty (only `.gitkeep`)
After moving YAML files into `internal/i18n/locales/`, the root `locales/`
directory serves no purpose. It could mislead contributors who expect to drop
translation files there.
**Severity: low** — cosmetic, but can confuse.

### 7. All `go.mod` deps are at minimum — no `gopkg.in/yaml.v3` in indirect list after tidy
After `go mod tidy` the indirect list is cleaner. No issue — just confirming.

---

## Refactor candidates

None of the findings above are severe enough to block progress, but findings
2 and 3 are worth fixing before production (steps 14/30):

- **05.5-refactor-i18n-bundle**: replace `init()`/`panic` with explicit
  `NewBundle()` constructor; wire it in `main.go`.
- No new tasks created at this stage per CLAUDE.md rule (refactors at multiples
  of 5 only if "worth doing as separate steps"). Finding #2 is worth it.

## Tasks queued
- `05.5-refactor-i18n-bundle.md` — replace panic-in-init with explicit
  constructor.
