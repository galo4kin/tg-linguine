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

	progress(onProgress, StageFetching)
	extracted, err := s.extractor.Extract(ctx, url)
	if err != nil {
		return nil, err
	}

	progress(onProgress, StageAnalyzing)
	resp, err := s.llm.Analyze(ctx, key, llm.AnalyzeRequest{
		TargetLanguage: active.LanguageCode,
		NativeLanguage: user.InterfaceLanguage,
		CEFR:           active.CEFRLevel,
		ArticleTitle:   extracted.Title,
		ArticleText:    extracted.Content,
	})
	if err != nil {
		return nil, err
	}

	progress(onProgress, StagePersisting)

	adapted, err := json.Marshal(resp.AdaptedVersions)
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
