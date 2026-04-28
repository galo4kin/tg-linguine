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
)

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
type AnalyzedArticle struct {
	Article *Article
	Words   []dictionary.DictionaryWord
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
	Log          *slog.Logger
}

func NewService(d ServiceDeps) *Service {
	return &Service{
		db: d.DB, users: d.Users, languages: d.Languages, keys: d.Keys,
		extractor: d.Extractor, llm: d.LLM,
		articles: d.Articles, dict: d.Dictionary, awords: d.ArticleWords, statuses: d.Statuses,
		log: d.Log,
	}
}

// AnalyzeArticle resolves the user's active language and Groq API key, fetches
// and analyzes the article, then atomically writes the article + words +
// dictionary entries. Progress is reported through onProgress (may be nil).
func (s *Service) AnalyzeArticle(ctx context.Context, userID int64, url string, onProgress ProgressFunc) (*AnalyzedArticle, error) {
	start := time.Now()

	active, err := s.languages.Active(ctx, userID)
	if err != nil {
		if errors.Is(err, users.ErrNotFound) {
			return nil, ErrNoActiveLanguage
		}
		return nil, fmt.Errorf("active language: %w", err)
	}

	user, err := s.users.ByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("load user: %w", err)
	}

	key, err := s.keys.Get(ctx, userID, users.ProviderGroq)
	if err != nil {
		if errors.Is(err, users.ErrNotFound) {
			return nil, ErrNoAPIKey
		}
		return nil, fmt.Errorf("api key: %w", err)
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
					"analysis_skipped_ms", time.Since(start).Milliseconds(),
				)
			}
			return &AnalyzedArticle{Article: existing, Words: words}, nil
		}
	}

	progress(onProgress, StageFetching)
	extracted, err := s.extractor.Extract(ctx, url)
	if err != nil {
		return nil, err
	}

	knownLemmas, err := s.statuses.KnownLemmas(ctx, s.db, userID, active.LanguageCode)
	if err != nil {
		return nil, fmt.Errorf("known lemmas: %w", err)
	}

	progress(onProgress, StageAnalyzing)
	resp, err := s.llm.Analyze(ctx, key, llm.AnalyzeRequest{
		TargetLanguage: active.LanguageCode,
		NativeLanguage: user.InterfaceLanguage,
		CEFR:           active.CEFRLevel,
		KnownWords:     knownLemmas,
		ArticleTitle:   extracted.Title,
		ArticleText:    extracted.Content,
	})
	if err != nil {
		return nil, err
	}

	progress(onProgress, StagePersisting)

	adapted, err := json.Marshal(adaptedFromLLM(active.CEFRLevel, resp.AdaptedVersions))
	if err != nil {
		return nil, fmt.Errorf("marshal adapted: %w", err)
	}

	article := &Article{
		UserID:          userID,
		SourceURL:       extracted.URL,
		SourceURLHash:   extracted.URLHash,
		Title:           extracted.Title,
		LanguageCode:    active.LanguageCode,
		CEFRDetected:    resp.CEFRDetected,
		SummaryTarget:   resp.SummaryTarget,
		SummaryNative:   resp.SummaryNative,
		AdaptedVersions: string(adapted),
	}

	storedWords := make([]dictionary.DictionaryWord, 0, len(resp.Words))

	err = WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if resp.Category != "" {
			catID, err := s.articles.UpsertCategory(ctx, tx, resp.Category)
			if err != nil {
				return err
			}
			article.CategoryID = catID
		}
		if err := s.articles.Insert(ctx, tx, article); err != nil {
			return err
		}
		for _, w := range resp.Words {
			dw := dictionary.DictionaryWord{
				LanguageCode:     active.LanguageCode,
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
			"words_count", len(storedWords),
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}

	return &AnalyzedArticle{Article: article, Words: storedWords}, nil
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
	if !IsCEFR(targetLevel) {
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
		return "", ErrNoSourceText
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
	if lvl, ok := CEFRShift(userLevel, -1); ok && v.Lower != "" {
		out[lvl] = v.Lower
	}
	if IsCEFR(userLevel) && v.Current != "" {
		out[userLevel] = v.Current
	}
	if lvl, ok := CEFRShift(userLevel, +1); ok && v.Higher != "" {
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
	for i, l := range CEFRLevels {
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
