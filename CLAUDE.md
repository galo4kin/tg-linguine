# Task workflow

Agreement with the user on how incoming specs are processed:

1. When the user sends a spec, break it down into steps and create a
   separate task file per step in `_10_todo/`.
2. Every task file has a numeric prefix that defines execution order:
   `01-<slug>.md`, `02-<slug>.md`, etc.
3. The body of a task file is a self-contained prompt for executing
   that step: goal, context, what to do, definition of done.
4. When the user (or the routine) picks up the next task:
   - move the file from `_10_todo/` to `_20_in_progress/` BEFORE
     starting any work;
   - after finishing, move it from `_20_in_progress/` to `_30_done/`;
   - keep the numeric prefix so order is preserved in history.
5. Always take tasks in ascending numeric order — never skip ahead.
6. Do not invent new "future" tasks without an explicit spec from the
   user (the only exception is refactor tasks created during a code
   review at multiples of 5 — see below).

`_10_todo/`, `_20_in_progress/`, `_30_done/` are the single source of
truth for the current state of work. The `_NN_` prefix exists so that
alphabetic sort matches the lifecycle order in any file manager / `ls`.

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

# Step execution rules

- **Lock**: at any moment `_20_in_progress/` must contain at most one
  task file. If there is already any `*.md` file there (other than
  `.gitkeep` or a `*-review.md`), it means a task is already in flight
  (manually, by another session, or by a previous routine run). Do not
  start a new task in that case.
- **Claim before work**: move the next-numbered file from `_10_todo/`
  to `_20_in_progress/` BEFORE making any code changes. The move is
  what counts as "taking the task".
- **Build-loop in DoD**: after applying changes, run `make build` and
  iterate on errors until the binary builds cleanly. A task is not
  complete without a green build. If the task also requires tests,
  `make test` must be green too.
- **Requirement compliance**: walk the DoD list in the task file and
  confirm each item is actually satisfied — not "probably" satisfied.
- **Closing a task**: move the file from `_20_in_progress/` to
  `_30_done/` under the same name, then make a single git commit with
  a clear message (`step NN: <slug>`).
- **Code review every 5 tasks**: after closing a task whose number is
  a multiple of 5 (`05`, `10`, `15`, …), review the project changes
  for refactor opportunities — dead code, duplication, bad names,
  leaky abstractions, unused dependencies, package-by-feature
  violations. Write findings to
  `_30_done/NN-<slug>-review.md`. If concrete refactors are worth
  doing as separate steps, create focused tasks in `_10_todo/` so they
  are picked up FIRST on the next run. Naming: take the smallest
  numeric prefix currently in `_10_todo/` (call it `NEXT`), use
  `(NEXT - 1).M-refactor-<slug>.md` where `M` starts at `5` and grows
  (`5`, `6`, `7`…) when several refactor tasks are queued at once.
  Example: if the next pending task is `08-foo.md`, the new refactor
  files are `07.5-refactor-<slug>.md`, `07.6-refactor-<slug>.md`, …
  This guarantees alphabetic sort puts them before `NEXT` so the next
  routine run starts with the refactor work.
- One step = one commit.
- **Rebuild → restart**: whenever `make build` produces a new
  `bin/tg-linguine` binary as part of a task (i.e. production code
  changed, not just tests/docs), kill the running bot so the cron
  watchdog respawns the new binary on the next tick. Do NOT start
  the bot manually — the watchdog at `scripts/linguine-watchdog.sh`
  runs every minute via crontab and is the only legitimate way to
  start the process. The full sequence is:
  ```sh
  pkill -f bin/tg-linguine   # watchdog will restart within 60s
  ```
  Verify with `ps aux | grep tg-linguine` after ~70s — a new PID
  means the new binary is up. Skip the kill only when the change is
  test-only or docs-only.
- Sibling reference project: `~/Projects/tg-boltun` (Go, flat layout,
  watchdog in cron). Use it as a reference for the watchdog script,
  deploy flow, and `.gitignore` practices.
