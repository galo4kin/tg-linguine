# Step 56: Fix Quiz Poll Mode UX

## Goal
Fix the native Telegram quiz poll mode so polls behave like inline cards:
previous messages are deleted on transitions, control buttons appear under
polls, and the poll is replaced by a feedback card after answering.

## Context
- Spec: docs/superpowers/specs/2026-04-29-fix-quiz-poll-mode-design.md
- Plan: docs/superpowers/plans/2026-04-29-fix-quiz-poll-mode.md

## What to do
See the implementation plan for detailed steps.

## Definition of Done
- [ ] `lastBotMessage` tracking added to Study struct
- [ ] Quiz polls render with skip/delete/end inline buttons
- [ ] HandlePollAnswer deletes poll message before sending feedback
- [ ] handleSkip/handleDelete/endAndSummarize branch for poll UIMode
- [ ] renderState deletes feedback message (not just clears keyboard) before poll
- [ ] touchLastSeenMiddleware handles PollAnswer updates
- [ ] `make build` green
- [ ] Manual verification: all 7 scenarios from spec pass
