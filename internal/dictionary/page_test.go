package dictionary_test

import (
	"context"
	"testing"

	"github.com/nikita/tg-linguine/internal/articles"
	"github.com/nikita/tg-linguine/internal/dictionary"
)

func TestArticleWords_PageAndCount(t *testing.T) {
	db := newTestDB(t)
	artRepo := articles.NewSQLiteRepository(db)
	dict := dictionary.NewSQLiteRepository(db)
	awords := dictionary.NewSQLiteArticleWordsRepository(db)
	ctx := context.Background()

	a := &articles.Article{UserID: 1, SourceURL: "u", SourceURLHash: "h", Title: "t", LanguageCode: "en"}
	if err := artRepo.Insert(ctx, db, a); err != nil {
		t.Fatalf("article: %v", err)
	}
	for i, lemma := range []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel", "india", "juliet", "kilo"} {
		wid, err := dict.UpsertLemma(ctx, db, dictionary.DictionaryWord{
			LanguageCode: "en", Lemma: lemma, POS: "noun", TranscriptionIPA: "/x/",
		})
		if err != nil {
			t.Fatalf("dict: %v", err)
		}
		if err := awords.Insert(ctx, db, dictionary.ArticleWord{
			ArticleID: a.ID, DictionaryWordID: wid,
			SurfaceForm:       lemma + "-s",
			TranslationNative: "перевод-" + lemma,
			ExampleTarget:     "example for " + lemma,
			ExampleNative:     "пример для " + lemma,
		}); err != nil {
			t.Fatalf("aw[%d]: %v", i, err)
		}
	}

	total, err := awords.CountByArticle(ctx, db, a.ID)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if total != 11 {
		t.Fatalf("expected 11, got %d", total)
	}

	page0, err := awords.PageByArticle(ctx, db, a.ID, 5, 0)
	if err != nil {
		t.Fatalf("page0: %v", err)
	}
	if len(page0) != 5 || page0[0].Lemma != "alpha" || page0[4].Lemma != "echo" {
		t.Fatalf("page0 unexpected: %+v", page0)
	}

	page1, err := awords.PageByArticle(ctx, db, a.ID, 5, 5)
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1) != 5 || page1[0].Lemma != "foxtrot" || page1[4].Lemma != "juliet" {
		t.Fatalf("page1 unexpected: %+v", page1)
	}

	page2, err := awords.PageByArticle(ctx, db, a.ID, 5, 10)
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2) != 1 || page2[0].Lemma != "kilo" {
		t.Fatalf("page2 unexpected: %+v", page2)
	}

	// Joined fields should round-trip.
	if page0[0].SurfaceForm != "alpha-s" || page0[0].TranslationNative != "перевод-alpha" {
		t.Fatalf("join fields lost: %+v", page0[0])
	}
}
