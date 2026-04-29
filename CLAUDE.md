# Stack and structure

- Language: **Go** (the initial TS+grammY skeleton was a mistake and is
  removed in step 1).
- Architecture: **package-by-feature** under `/internal/` (`users`,
  `articles`, `dictionary`, `llm`, `telegram`, `storage`, `i18n`,
  `crypto`, `session`, `config`, `logger`). Entry point:
  `/cmd/bot/main.go`.
- Database: SQLite via `modernc.org/sqlite` (pure Go, no CGO);
  migrations via `golang-migrate` with `embed`.
- Telegram: `go-telegram/bot`, long-polling.
- LLM: Groq via the OpenAI-compatible client.
- Config and logs: `caarlos0/env` + `godotenv`, `slog` + `lumberjack`.
- Deployment: mac mini, watchdog script modeled after
  `~/Projects/tg-boltun/boltun-watchdog.sh`, run from crontab.
- Sibling reference project: `~/Projects/tg-boltun` (Go, flat layout,
  watchdog in cron). Use it as a reference for the watchdog script,
  deploy flow, and `.gitignore` practices.

# Workflow

- **Small tasks** (a focused edit in one or two files) — just do them.
  No ceremony, no TodoWrite required.
- **Large tasks** (several files, new functionality, multi-hour work) —
  first draft an implementation plan via the `superpowers:writing-plans`
  skill, break it into steps with `TodoWrite`, and execute independent
  steps via subagents when possible
  (`superpowers:subagent-driven-development` /
  `superpowers:dispatching-parallel-agents`).
- **One logical step = one commit** with a clear message.
- **TodoWrite is the source of truth for in-flight steps.** No task
  folders, no per-step markdown files on disk — history of completed
  work lives in `git log`.

# Build and verification

- After applying changes, run `make build` and iterate on errors until
  the binary builds cleanly. A change is not done without a green
  build. If tests are part of the work, `make test` must be green too.
- Walk the user's stated requirements / DoD and confirm each item is
  actually satisfied — not "probably" satisfied.

# Periodic code review

Two triggers, both run as a quick refactor-oriented review of the
recent diff (look for dead code, duplication, bad names, leaky
abstractions, unused dependencies, package-by-feature violations):

- **Every ~5 commits of production code** — before starting the next
  step, check `git log` since the last `review:` commit (or since the
  branch base). If ≥5 production commits have accumulated without a
  review, do one. Commit findings as `review: <topic>`. Apply trivial
  fixes inline; substantial refactors become their own planned step.
- **At the end of a large plan** — once every step in the current plan
  is closed, do a final review of the plan's overall diff.

# Rebuild → restart

Whenever `make build` produces a new `bin/tg-linguine` binary as part
of a change (i.e. production code changed, not just tests/docs), kill
the running bot so the cron watchdog respawns the new binary on the
next tick. Do NOT start the bot manually — the watchdog at
`scripts/linguine-watchdog.sh` runs every minute via crontab and is
the only legitimate way to start the process. The full sequence is:

```sh
pkill -f bin/tg-linguine   # watchdog will restart within 60s
```

Verify with `ps aux | grep tg-linguine` after ~70s — a new PID means
the new binary is up. Skip the kill only when the change is test-only
or docs-only.
