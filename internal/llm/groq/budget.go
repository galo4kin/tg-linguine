package groq

import "time"

// Free-tier TPM (tokens-per-minute) budget for the Groq plan we currently
// run on. Each chat request bills input tokens + the model's reserved
// output tokens against this minute window, so the various per-call caps
// must add up to less than FreeTierTPM with a margin for the system prompt
// and JSON scaffolding (~1K). Increase the caps together when moving to a
// paid tier.
//
// Layout of the analyze request at default sizing:
//
//	DefaultArticleInputCap (5000) +
//	ArticleAnalyzeOutputCap (4000) +
//	system prompt + scaffolding (~1000) ≈ 10K (under 12K TPM)
//
// Layout of the summarize request:
//
//	SummarizeInputCap (7500) +
//	caller-chosen output (typically TargetTokens+500 ≤ 3000) +
//	system prompt (~500) ≈ 11K (under 12K TPM)
const (
	FreeTierTPM = 12000

	// DefaultArticleInputCap is the per-article input budget enforced by
	// articles.Service when ServiceDeps.MaxTokens is left at zero. Articles
	// whose extracted body exceeds this cap are parked and offered to the
	// user as truncate / pre-summary.
	DefaultArticleInputCap = 5000

	// ArticleAnalyzeOutputCap is the max_tokens we send on the JSON
	// analysis chat call. Large enough to fit summary_target +
	// summary_native + adapted versions + words list without truncation,
	// small enough to keep the request inside FreeTierTPM.
	ArticleAnalyzeOutputCap = 4000

	// SummarizeInputCap is the input-side cap for the pre-summary chat
	// call (long-article path, ModeSummarize). The Telegram layer
	// truncates the article body to this many tokens before sending it to
	// the summarizer.
	SummarizeInputCap = 7500

	// MaxRetryAfter caps the wait we'll accept from Groq's Retry-After
	// signal before bailing out and reporting ErrRateLimited. Anything
	// longer would pin a Telegram handler goroutine for minutes.
	MaxRetryAfter = 60 * time.Second

	// RetryAfterBuffer is added to Groq's parsed Retry-After hint before
	// sleeping. Groq's number sometimes underestimates by a fraction of a
	// second — the precision is fine, but the rolling-minute window has
	// not quite slid by the time the retry lands. The buffer keeps a
	// single retry sufficient in practice.
	RetryAfterBuffer = 750 * time.Millisecond

	// MaxRateLimitAttempts caps the total number of attempts (initial +
	// retries) per chat call. Free-tier Groq sometimes needs two retries
	// when summarize and analyze land in the same TPM minute; four
	// attempts gives us room to recover without ever waiting longer than
	// MaxRetryAfter cumulatively.
	MaxRateLimitAttempts = 4
)
