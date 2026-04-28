# Step 45: Words page size → 3

## Goal
Reduce words displayed per page from 5 to 3 so the screen fits on a phone.

## What to do
In `internal/telegram/handlers/words.go` line 20, change:
```go
const wordsPageSize = 5
```
to:
```go
const wordsPageSize = 3
```

That's the only code change. The pagination logic is generic and works with any page size.

## Definition of done
- [ ] `wordsPageSize` constant equals 3
- [ ] `make build` passes
- [ ] Commit `step 45: words-page-size-3`
