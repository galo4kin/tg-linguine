# Review 60: vocab extraction improvement (steps 59-60)

## Findings (all fixed inline)

1. **extractVocabChunks never returned errors** — function always returned nil error
   even when all chunks failed. Fixed: returns error when all chunks fail.

2. **chunkText used fragile strings.Index** — substring search could match at wrong
   position with repeated paragraphs. Fixed: use strings.HasPrefix since chunk is
   always a prefix of remaining.

3. **Parameter `cap` shadowed Go builtin** — renamed to `limit` in mergeWords.

4. **Inaccurate comment** — said "known lemmas + already found" but exclude only
   contained already-found (knownLemmas go in KnownWords field). Fixed comment.

5. **500-word instruction buried mid-sentence** — split into its own bullet point
   in system.txt for better LLM attention.

## No refactor tasks needed

The code is clean and well-structured. No new refactor tasks created.
