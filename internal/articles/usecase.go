package articles

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/nikita/tg-linguine/internal/dictionary"
	"github.com/nikita/tg-linguine/internal/llm"
	"github.com/nikita/tg-linguine/internal/users"
)

var (
	ErrNoActiveLanguage = errors.New("articles: user has no active language")
	ErrNoAPIKey         = errors.New("articles: user has no api key")
	ErrNoSourceText     = errors.New("articles: no adapted text to use as regen source")
	ErrUnknownCEFR      = errors.New("articles: unknown cefr level")
	// ErrPendingExpired is raised by AnalyzeExtracted when the supplied
	// PendingID is unknown — either the TTL fired or the same id was
	// already consumed by a previous click. The Telegram layer maps this
	// to a friendly "session expired, resend the link" message.
	ErrPendingExpired = errors.New("articles: pending session expired")
)

// DefaultMaxTokensPerArticle is the fallback used when ServiceDeps.MaxTokens
// is left at zero. 30000 fits well within the 128K context of
// llama-3.3-70b-versatile while leaving headroom for prompts, the JSON
// response, and three CEFR-level adapted versions.
const DefaultMaxTokensPerArticle = 30000

// summarizeInputBudget caps the tokens we feed into the pre-summary call
// itself. Even a 128K-context model degrades on extremely long inputs; this
// keeps the summarize prompt comfortably under that ceiling.
const summarizeInputBudget = 100000

// LongAnalysisMode is selected by the user when an article exceeds the
// per-request token budget. Both modes always produce a stored article;
// they differ only in how the extracted text is shaped before analysis.
type LongAnalysisMode int

const (
	// ModeTruncate keeps the first paragraphs of the article up to the
	// budget. Cheapest variant — one LLM call, identical to the normal
	// flow but on a paragraph-cut prefix.
	ModeTruncate LongAnalysisMode = iota + 1
	// ModeSummarize asks the LLM to compress the article (in the original
	// language) so that nothing is dropped wholesale, then runs the normal
	// analysis on the summary. Two LLM calls.
	ModeSummarize
)

// LongPending describes a parked article that exceeded the token budget
// during AnalyzeArticle. The Telegram layer renders a prompt with two
// inline-keyboard buttons whose callback_data carries the PendingID.
type LongPending struct {
	PendingID string
	Tokens    int
	Words     int
	Limit     int
}

// AnalyzeResult is the structured return of AnalyzeArticle. Exactly one of
// {Article, LongPending} is non-nil on a successful return; an error means
// neither is meaningful.
type AnalyzeResult struct {
	Article     *AnalyzedArticle
	LongPending *LongPending
}

// ApproxWordCount is purely for the user-facing rejection message — we tell
// the user how many words their article is so they have a sense of how much
// to trim. strings.Fields gives a good-enough split for any whitespace.
func ApproxWordCount(text string) int {
	if text == "" {
		return 0
	}
	count := 0
	inWord := false
	for _, r := range text {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			inWord = false
			continue
		}
		if !inWord {
			count++
			inWord = true
		}
	}
	return count
}

// Stage notifies the caller of progress; used by the Telegram handler to
// edit the status message between long-running steps.
type Stage int

const (
	StageFetching Stage = iota + 1
	StageAnalyzing
	StagePersisting
)

type ProgressFunc func(Stage)

// AnalyzedArticle bundles the freshly stored Article with the words that were
// recorded for it (in the same order the LLM emitted), so the caller can
// render the article card without an extra DB round-trip.
//
// Notice is non-empty when the analysis ran on a transformed body
// (paragraph-truncated or LLM-pre-summarized) so the renderer can prepend
// a single-line banner above the card. Empty for normal full-article
// analyses and for cache replays.
type AnalyzedArticle struct {
	Article *Article
	Words   []dictionary.DictionaryWord
	Notice  string
}

// Service performs the full URL → analysis → storage pipeline.
type Service struct {
	db        *sql.DB
	users     *users.Service
	languages users.UserLanguageRepository
	keys      users.APIKeyRepository
	extractor Extractor
	llm       llm.Provider
	articles  Repository
	dict      dictionary.Repository
	awords    dictionary.ArticleWordsRepository
	statuses  dictionary.UserWordStatusRepository
	maxTokens int
	blocklist *Blocklist
	pending   *PendingStore
	log       *slog.Logger
}

type ServiceDeps struct {
	DB           *sql.DB
	Users        *users.Service
	Languages    users.UserLanguageRepository
	Keys         users.APIKeyRepository
	Extractor    Extractor
	LLM          llm.Provider
	Articles     Repository
	Dictionary   dictionary.Repository
	ArticleWords dictionary.ArticleWordsRepository
	Statuses     dictionary.UserWordStatusRepository
	// MaxTokens caps the estimated token count of an article before we
	// invoke the LLM. Zero means use DefaultMaxTokensPerArticle.
	MaxTokens int
	// Blocklist gates URLs by host before the network call. Nil means
	// "no blocking" — used by tests that don't care about safety.
	Blocklist *Blocklist
	Log       *slog.Logger
}

func NewService(d ServiceDeps) *Service {
	maxTokens := d.MaxTokens
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokensPerArticle
	}
	return &Service{
		db: d.DB, users: d.Users, languages: d.Languages, keys: d.Keys,
		extractor: d.Extractor, llm: d.LLM,
		articles: d.Articles, dict: d.Dictionary, awords: d.ArticleWords, statuses: d.Statuses,
		maxTokens: maxTokens,
		blocklist: d.Blocklist,
		pending:   NewPendingStore(0),
		log:       d.Log,
	}
}


// AnalyzeArticle resolves the user's active language and Groq API key, fetches
// and analyzes the article, then atomically writes the article + words +
// dictionary entries. Progress is reported through onProgress (may be nil).
//
// When the extracted body exceeds the per-article token budget, the article
// is parked in the in-memory pending store and the function returns an
// AnalyzeResult whose LongPending is non-nil. The Telegram layer presents
// the user with a choice (truncate vs pre-summary) and re-enters via
// AnalyzeExtracted.
func (s *Service) AnalyzeArticle(ctx context.Context, userID int64, url string, onProgress ProgressFunc) (*AnalyzeResult, error) {
	start := time.Now()

	active, err := s.languages.Active(ctx, userID)
	if err != nil {
		if errors.Is(err, users.ErrNotFound) {
			return nil, ErrNoActiveLanguage
		}
		return nil, fmt.Errorf("active language: %w", err)
	}

	if _, err := s.users.ByID(ctx, userID); err != nil {
		return nil, fmt.Errorf("load user: %w", err)
	}

	if _, err := s.keys.Get(ctx, userID, users.ProviderGroq); err != nil {
		if errors.Is(err, users.ErrNotFound) {
			return nil, ErrNoAPIKey
		}
		return nil, fmt.Errorf("api key: %w", err)
	}

	// Source-domain blocklist: enforced BEFORE the cache lookup so a
	// previously analyzed URL that has since been added to the blocklist
	// cannot be replayed from cache. The check is host-only — `Contains`
	// handles subdomain suffix matching internally.
	if s.blocklist != nil && s.blocklist.MatchURL(url) {
		if s.log != nil {
			s.log.Info("article rejected: blocked source",
				"user_id", userID,
				"url", url,
				"extractor_called", false,
				"reason", "blocked_source_domain",
			)
		}
		return nil, ErrBlockedSource
	}

	// Cache hit: same user + same normalized URL hash + same target language —
	// and same detected CEFR as the user's current level — replays the stored
	// article card without invoking the extractor or LLM. Mismatched CEFR or
	// failed normalization falls through to the full pipeline.
	if normalized, normErr := NormalizeURL(url); normErr == nil {
		hash := URLHash(normalized)
		existing, lookupErr := s.articles.ByUserAndHash(ctx, s.db, userID, hash, active.LanguageCode)
		if lookupErr != nil && !errors.Is(lookupErr, ErrNotFound) {
			return nil, fmt.Errorf("cache lookup: %w", lookupErr)
		}
		if existing != nil {
			words, err := s.loadStoredWords(ctx, existing)
			if err != nil {
				return nil, err
			}
			if s.log != nil {
				s.log.Info("article reused",
					"user_id", userID,
					"article_id", existing.ID,
					"cache_hit", true,
					"analysis_duration_ms", time.Since(start).Milliseconds(),
				)
			}
			return &AnalyzeResult{Article: &AnalyzedArticle{Article: existing, Words: words}}, nil
		}
	}

	progress(onProgress, StageFetching)
	extracted, err := s.extractor.Extract(ctx, url)
	if err != nil {
		return nil, err
	}

	// Token-budget gate: when the extracted body cannot fit, park it and let
	// the caller present a choice. We never reject outright — every link the
	// user sends should yield a usable analysis, possibly on a transformed
	// version of the text.
	tokensEstimated := llm.EstimateTokens(extracted.Content)
	if tokensEstimated > s.maxTokens {
		words := ApproxWordCount(extracted.Content)
		pendingID := s.pending.Put(userID, extracted)
		if s.log != nil {
			s.log.Info("article parked: too long",
				"user_id", userID,
				"url", url,
				"tokens_estimated", tokensEstimated,
				"tokens_limit", s.maxTokens,
				"words_estimated", words,
				"pending_id", pendingID,
			)
		}
		return &AnalyzeResult{LongPending: &LongPending{
			PendingID: pendingID,
			Tokens:    tokensEstimated,
			Words:     words,
			Limit:     s.maxTokens,
		}}, nil
	}

	analyzed, err := s.runAnalysis(ctx, userID, active.LanguageCode, active.CEFRLevel, extracted, "", onProgress, start, tokensEstimated)
	if err != nil {
		return nil, err
	}
	return &AnalyzeResult{Article: analyzed}, nil
}

// AnalyzeExtracted resumes a parked long-article session: it pops the
// extracted body from the pending store, transforms it according to mode
// (truncate or LLM pre-summary), and runs the standard analysis pipeline
// against the transformed body. The returned AnalyzedArticle carries a
// localized banner string in Notice.
//
// noticeRenderer maps a NoticeKind into a localized one-line string; the
// Telegram layer plugs in its i18n bundle. Passing nil yields an empty
// notice — useful for tests that don't care about the user-facing wording.
type NoticeKind int

const (
	NoticeTruncated NoticeKind = iota + 1
	NoticeSummarized
)

// NoticeData carries the numbers that go into the localized banner.
type NoticeData struct {
	Kind       NoticeKind
	Percent    int
	Words      int
	TotalWords int
}

// NoticeRenderer is implemented by the Telegram layer to render a localized
// banner string from NoticeData. Passing nil to AnalyzeExtracted yields an
// empty notice.
type NoticeRenderer interface {
	RenderNotice(NoticeData) string
}

func (s *Service) AnalyzeExtracted(ctx context.Context, userID int64, pendingID string, mode LongAnalysisMode, notice NoticeRenderer, onProgress ProgressFunc) (*AnalyzedArticle, error) {
	start := time.Now()

	extracted, ok := s.pending.Take(pendingID, userID)
	if !ok {
		return nil, ErrPendingExpired
	}

	active, err := s.languages.Active(ctx, userID)
	if err != nil {
		if errors.Is(err, users.ErrNotFound) {
			return nil, ErrNoActiveLanguage
		}
		return nil, fmt.Errorf("active language: %w", err)
	}

	totalWords := ApproxWordCount(extracted.Content)
	transformed := extracted
	noticeText := ""

	switch mode {
	case ModeTruncate:
		body, percent := TruncateAtParagraph(extracted.Content, s.maxTokens)
		transformed.Content = body
		if notice != nil {
			noticeText = notice.RenderNotice(NoticeData{
				Kind:       NoticeTruncated,
				Percent:    percent,
				Words:      ApproxWordCount(body),
				TotalWords: totalWords,
			})
		}
		if s.log != nil {
			s.log.Info("article truncated",
				"user_id", userID,
				"pending_id", pendingID,
				"percent_kept", percent,
			)
		}
	case ModeSummarize:
		// Pre-summary needs the API key here so we can send the compress
		// call before reaching runAnalysis.
		key, err := s.keys.Get(ctx, userID, users.ProviderGroq)
		if err != nil {
			if errors.Is(err, users.ErrNotFound) {
				return nil, ErrNoAPIKey
			}
			return nil, fmt.Errorf("api key: %w", err)
		}
		// Cap the input to the summarize call so an outlier-sized article
		// does not blow past the model's context window on the way in.
		summarizeIn, _ := TruncateAtParagraph(extracted.Content, summarizeInputBudget)
		summary, err := s.llm.Summarize(ctx, key, llm.SummarizeRequest{
			TargetLanguage: active.LanguageCode,
			ArticleTitle:   extracted.Title,
			ArticleText:    summarizeIn,
			TargetTokens:   s.maxTokens - 1000,
		})
		if err != nil {
			return nil, err
		}
		// Belt-and-braces: even after summarization, paragraph-truncate to
		// the budget so a verbose summary cannot trip the analysis cap.
		body, _ := TruncateAtParagraph(summary, s.maxTokens)
		transformed.Content = body
		if notice != nil {
			noticeText = notice.RenderNotice(NoticeData{
				Kind:       NoticeSummarized,
				TotalWords: totalWords,
			})
		}
		if s.log != nil {
			s.log.Info("article summarized",
				"user_id", userID,
				"pending_id", pendingID,
				"orig_words", totalWords,
				"summary_chars", len([]rune(body)),
			)
		}
	default:
		return nil, fmt.Errorf("articles: unknown long-analysis mode %d", mode)
	}

	tokens := llm.EstimateTokens(transformed.Content)
	return s.runAnalysis(ctx, userID, active.LanguageCode, active.CEFRLevel, transformed, noticeText, onProgress, start, tokens)
}

// runAnalysis is the shared back half of the URL pipeline used by both the
// normal AnalyzeArticle path and the AnalyzeExtracted (truncate/summarize)
// path. It calls the LLM, persists the result, and returns the freshly-stored
// article. Caller has already checked language/api-key/blocklist/cache and
// trimmed the body to the budget.
func (s *Service) runAnalysis(ctx context.Context, userID int64, languageCode, userCEFR string, extracted Extracted, notice string, onProgress ProgressFunc, start time.Time, tokensEstimated int) (*AnalyzedArticle, error) {
	user, err := s.users.ByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("load user: %w", err)
	}
	key, err := s.keys.Get(ctx, userID, users.ProviderGroq)
	if err != nil {
		return nil, fmt.Errorf("api key: %w", err)
	}

	knownLemmas, err := s.statuses.KnownLemmas(ctx, s.db, userID, languageCode)
	if err != nil {
		return nil, fmt.Errorf("known lemmas: %w", err)
	}

	progress(onProgress, StageAnalyzing)
	resp, err := s.llm.Analyze(ctx, key, llm.AnalyzeRequest{
		TargetLanguage: languageCode,
		NativeLanguage: user.InterfaceLanguage,
		CEFR:           userCEFR,
		KnownWords:     knownLemmas,
		ArticleTitle:   extracted.Title,
		ArticleText:    extracted.Content,
	})
	if err != nil {
		return nil, err
	}

	// LLM-side safety gate: a non-empty `safety_flags` array means the model
	// itself classified the content as adult / illegal / otherwise unsafe.
	// We drop the analysis on the floor — nothing is persisted to articles or
	// dictionary, so a flagged piece never leaks into history or vocabulary.
	if len(resp.SafetyFlags) > 0 {
		if s.log != nil {
			s.log.Info("article rejected: safety flags",
				"user_id", userID,
				"url", extracted.URL,
				"safety_flags", resp.SafetyFlags,
				"persisted", false,
				"reason", "llm_safety_flagged",
			)
		}
		return nil, ErrBlockedContent
	}

	progress(onProgress, StagePersisting)

	adapted, err := json.Marshal(adaptedFromLLM(userCEFR, resp.AdaptedVersions))
	if err != nil {
		return nil, fmt.Errorf("marshal adapted: %w", err)
	}

	article := &Article{
		UserID:          userID,
		SourceURL:       extracted.URL,
		SourceURLHash:   extracted.URLHash,
		Title:           extracted.Title,
		LanguageCode:    languageCode,
		CEFRDetected:    resp.CEFRDetected,
		SummaryTarget:   resp.SummaryTarget,
		SummaryNative:   resp.SummaryNative,
		AdaptedVersions: string(adapted),
	}

	storedWords := make([]dictionary.DictionaryWord, 0, len(resp.Words))

	// Always normalize to one of CategoryCodes; the LLM occasionally returns
	// freeform values or omits the field, and downstream filters expect a
	// known code (or empty for "no category").
	category := NormalizeCategory(resp.Category)

	err = WithTx(ctx, s.db, func(tx *sql.Tx) error {
		catID, err := s.articles.UpsertCategory(ctx, tx, category)
		if err != nil {
			return err
		}
		article.CategoryID = catID
		article.Category = category
		if err := s.articles.Insert(ctx, tx, article); err != nil {
			return err
		}
		for _, w := range resp.Words {
			dw := dictionary.DictionaryWord{
				LanguageCode:     languageCode,
				Lemma:            w.Lemma,
				POS:              w.POS,
				TranscriptionIPA: w.TranscriptionIPA,
			}
			id, err := s.dict.UpsertLemma(ctx, tx, dw)
			if err != nil {
				return err
			}
			dw.ID = id
			if err := s.awords.Insert(ctx, tx, dictionary.ArticleWord{
				ArticleID:         article.ID,
				DictionaryWordID:  id,
				SurfaceForm:       w.SurfaceForm,
				TranslationNative: w.TranslationNative,
				ExampleTarget:     w.ExampleTarget,
				ExampleNative:     w.ExampleNative,
			}); err != nil {
				return err
			}
			if err := s.statuses.Upsert(ctx, tx, dictionary.UserWordStatus{
				UserID:           userID,
				DictionaryWordID: id,
				Status:           dictionary.StatusLearning,
			}); err != nil {
				return err
			}
			storedWords = append(storedWords, dw)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if s.log != nil {
		s.log.Info("article analyzed",
			"user_id", userID,
			"article_id", article.ID,
			"article_chars", len([]rune(extracted.Content)),
			"tokens_estimated", tokensEstimated,
			"words_count", len(storedWords),
			"cache_hit", false,
			"analysis_duration_ms", time.Since(start).Milliseconds(),
		)
	}

	return &AnalyzedArticle{Article: article, Words: storedWords, Notice: notice}, nil
}

func progress(p ProgressFunc, s Stage) {
	if p != nil {
		p(s)
	}
}

// Adapt fills in a missing per-level adaptation for a stored article. It
// resolves the user's API key, picks the closest available source text from
// the article's existing adaptations, calls the LLM mini-prompt, and merges
// the result into the article's adapted_versions JSON. Returns the freshly
// generated text (or the cached one if the level was already present, which
// makes this idempotent).
func (s *Service) Adapt(ctx context.Context, userID, articleID int64, targetLevel string) (string, error) {
	if !users.IsCEFR(targetLevel) {
		return "", ErrUnknownCEFR
	}

	article, err := s.articles.ByID(ctx, s.db, articleID)
	if err != nil {
		return "", err
	}
	if article.UserID != userID {
		return "", ErrNotFound
	}

	current := article.ParseAdaptedVersions()
	if v, ok := current[targetLevel]; ok && v != "" {
		return v, nil
	}

	user, err := s.users.ByID(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("load user: %w", err)
	}
	key, err := s.keys.Get(ctx, userID, users.ProviderGroq)
	if err != nil {
		if errors.Is(err, users.ErrNotFound) {
			return "", ErrNoAPIKey
		}
		return "", fmt.Errorf("api key: %w", err)
	}

	sourceText, sourceCEFR := pickAdaptSource(current, targetLevel, article.CEFRDetected)
	if sourceText == "" {
		// Soft migration for articles persisted before adapted_versions.current
		// was required to be non-empty: fall back to the article's own target-
		// language summary as a regen source. summary_target is required by the
		// schema, so it is guaranteed non-empty for any stored article. Without
		// this fallback those records would be permanently unrenderable.
		if article.SummaryTarget == "" {
			return "", ErrNoSourceText
		}
		sourceText = article.SummaryTarget
		sourceCEFR = article.CEFRDetected
	}

	resp, err := s.llm.Adapt(ctx, key, llm.AdaptRequest{
		TargetLanguage: article.LanguageCode,
		NativeLanguage: user.InterfaceLanguage,
		TargetCEFR:     targetLevel,
		SourceCEFR:     sourceCEFR,
		SourceText:     sourceText,
	})
	if err != nil {
		return "", err
	}

	current[targetLevel] = resp.AdaptedText
	raw, err := json.Marshal(current)
	if err != nil {
		return "", fmt.Errorf("articles: marshal adapted: %w", err)
	}
	if err := s.articles.UpdateAdaptedVersions(ctx, s.db, articleID, string(raw)); err != nil {
		return "", err
	}
	if s.log != nil {
		s.log.Info("article adapted",
			"user_id", userID,
			"article_id", articleID,
			"target_cefr", targetLevel,
			"source_cefr", sourceCEFR,
		)
	}
	return resp.AdaptedText, nil
}

// adaptedFromLLM converts the LLM's relative {lower, current, higher} reply
// into the absolute CEFR-keyed map we persist. Empty strings (LLM output for
// out-of-range slots like "lower" at A1) are dropped so the renderer treats
// those slots as missing rather than empty-strings.
func adaptedFromLLM(userLevel string, v llm.AdaptedVersions) AdaptedVersions {
	out := AdaptedVersions{}
	if lvl, ok := users.CEFRShift(userLevel, -1); ok && v.Lower != "" {
		out[lvl] = v.Lower
	}
	if users.IsCEFR(userLevel) && v.Current != "" {
		out[userLevel] = v.Current
	}
	if lvl, ok := users.CEFRShift(userLevel, +1); ok && v.Higher != "" {
		out[lvl] = v.Higher
	}
	return out
}

// pickAdaptSource picks the best available adapted text to feed back into
// the LLM as a regen source. Preference order: closest absolute level to
// `target` (by CEFR distance), with ties broken in favor of the lower level
// — empirically the LLM has an easier time adapting upward than downward.
// If the absolute-keyed map is empty, returns ("", "") and the caller bails.
func pickAdaptSource(current AdaptedVersions, target, articleCEFR string) (text, sourceCEFR string) {
	if len(current) == 0 {
		return "", ""
	}
	ti := indexOfCEFR(target)
	if ti < 0 {
		// Unknown target — return any non-empty entry.
		for k, v := range current {
			if v != "" {
				return v, k
			}
		}
		return "", ""
	}
	bestIdx := -1
	for k, v := range current {
		if v == "" {
			continue
		}
		ki := indexOfCEFR(k)
		if ki < 0 {
			continue
		}
		if bestIdx < 0 || abs(ki-ti) < abs(bestIdx-ti) ||
			(abs(ki-ti) == abs(bestIdx-ti) && ki < bestIdx) {
			bestIdx = ki
			text = v
			sourceCEFR = k
		}
	}
	if text == "" && articleCEFR != "" {
		// Fall back to whatever absolute level the article was originally
		// detected at — caller may pass an empty hint.
		sourceCEFR = articleCEFR
	}
	return text, sourceCEFR
}

func indexOfCEFR(s string) int {
	for i, l := range users.CEFRLevels {
		if l == s {
			return i
		}
	}
	return -1
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// loadStoredWords reconstructs the DictionaryWord slice for a previously
// analyzed article so the caller can rebuild the article card without going
// through the LLM. Order matches the original insertion (article_words.rowid).
func (s *Service) loadStoredWords(ctx context.Context, article *Article) ([]dictionary.DictionaryWord, error) {
	total, err := s.awords.CountByArticle(ctx, s.db, article.ID)
	if err != nil {
		return nil, fmt.Errorf("cache: count words: %w", err)
	}
	if total == 0 {
		return nil, nil
	}
	views, err := s.awords.PageByArticle(ctx, s.db, article.ID, total, 0)
	if err != nil {
		return nil, fmt.Errorf("cache: load words: %w", err)
	}
	out := make([]dictionary.DictionaryWord, 0, len(views))
	for _, v := range views {
		out = append(out, dictionary.DictionaryWord{
			ID:               v.DictionaryWordID,
			LanguageCode:     article.LanguageCode,
			Lemma:            v.Lemma,
			POS:              v.POS,
			TranscriptionIPA: v.TranscriptionIPA,
		})
	}
	return out, nil
}
