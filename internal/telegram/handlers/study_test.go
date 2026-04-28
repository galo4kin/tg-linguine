package handlers

import (
	"strings"
	"testing"

	tgi18n "github.com/nikita/tg-linguine/internal/i18n"
	"github.com/nikita/tg-linguine/internal/session"
	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

// nullLocalizer returns the message ID for every key — sufficient for
// structural keyboard tests that only care about layout, not translated text.
func nullLocalizer() *goi18n.Localizer {
	bundle := goi18n.NewBundle(language.English)
	return goi18n.NewLocalizer(bundle)
}

// ruLocalizer returns a real Russian localizer backed by the embedded locale files.
func ruLocalizer(t *testing.T) *goi18n.Localizer {
	t.Helper()
	bundle, err := tgi18n.NewBundle()
	if err != nil {
		t.Fatalf("load i18n bundle: %v", err)
	}
	return tgi18n.For(bundle, "ru")
}

func TestQuizQuestionKeyboard_TwoOptionsPerRow(t *testing.T) {
	loc := nullLocalizer()
	snap := session.QuizSnapshot{
		Deck: []session.QuizCard{{
			DictionaryWordID: 1,
			Options:          []string{"alpha", "beta", "gamma", "delta"},
			CorrectIndex:     0,
		}},
		Cursor: 0,
	}

	kb := quizQuestionKeyboard(loc, snap)

	// First 2 rows must be answer options, each with exactly 2 buttons.
	answerRows := kb.InlineKeyboard[:2]
	for i, row := range answerRows {
		if len(row) != 2 {
			t.Errorf("answer row %d: want 2 buttons, got %d", i, len(row))
		}
	}

	// Last row must contain Skip, Delete and End buttons side by side.
	lastRow := kb.InlineKeyboard[len(kb.InlineKeyboard)-1]
	if len(lastRow) != 3 {
		t.Errorf("last row (skip+del+end): want 3 buttons, got %d", len(lastRow))
	}
	if lastRow[0].CallbackData != CallbackPrefixStudy+"skip" {
		t.Errorf("last row[0]: want skip callback, got %q", lastRow[0].CallbackData)
	}
	if !strings.HasPrefix(lastRow[1].CallbackData, CallbackPrefixStudy+"del:") {
		t.Errorf("last row[1]: want del: callback, got %q", lastRow[1].CallbackData)
	}
	if lastRow[2].CallbackData != CallbackPrefixStudy+"end" {
		t.Errorf("last row[2]: want end callback, got %q", lastRow[2].CallbackData)
	}
}

func TestQuizQuestionKeyboard_NoNumberPrefixOnOptions(t *testing.T) {
	loc := nullLocalizer()
	snap := session.QuizSnapshot{
		Deck: []session.QuizCard{{
			DictionaryWordID: 1,
			Options:          []string{"alpha", "beta", "gamma", "delta"},
			CorrectIndex:     0,
		}},
		Cursor: 0,
	}

	kb := quizQuestionKeyboard(loc, snap)

	for ri, row := range kb.InlineKeyboard[:2] {
		for bi, btn := range row {
			for _, opt := range snap.Deck[0].Options {
				if btn.Text == opt {
					goto ok
				}
			}
			t.Errorf("row %d btn %d text %q does not exactly match any option", ri, bi, btn.Text)
		ok:
		}
	}
}

func TestQuizQuestionKeyboard_OptionCallbacksEncode4Options(t *testing.T) {
	loc := nullLocalizer()
	snap := session.QuizSnapshot{
		Deck: []session.QuizCard{{
			DictionaryWordID: 7,
			Options:          []string{"a", "b", "c", "d"},
			CorrectIndex:     2,
		}},
		Cursor: 0,
	}

	kb := quizQuestionKeyboard(loc, snap)

	var answerBtns []string
	for _, row := range kb.InlineKeyboard[:2] {
		for _, btn := range row {
			answerBtns = append(answerBtns, btn.CallbackData)
		}
	}
	if len(answerBtns) != 4 {
		t.Fatalf("want 4 answer buttons, got %d", len(answerBtns))
	}
	for i, cb := range answerBtns {
		if !strings.Contains(cb, "ans:7:") {
			t.Errorf("button %d callback %q missing ans:7:", i, cb)
		}
	}
}

func TestRenderQuizFeedback_CorrectContainsGreenEmoji(t *testing.T) {
	loc := ruLocalizer(t)
	snap := session.QuizSnapshot{
		Deck:   []session.QuizCard{{Options: []string{"дом", "бежать", "учить", "дерево"}, CorrectIndex: 0}},
		Cursor: 0,
	}
	card := session.QuizCard{
		Lemma: "house", Direction: session.QuizForeignToNative,
		Options: []string{"дом", "бежать", "учить", "дерево"}, CorrectIndex: 0,
	}

	text := renderQuizFeedback(loc, snap, card, 0, true, false, nil)
	if !strings.Contains(text, "✅") {
		t.Errorf("correct feedback must contain ✅, got: %q", text)
	}
	if !strings.Contains(text, "<code>") {
		t.Errorf("word must be wrapped in <code>, got: %q", text)
	}
}

func TestRenderQuizFeedback_WrongContainsRedEmoji(t *testing.T) {
	loc := ruLocalizer(t)
	snap := session.QuizSnapshot{
		Deck:   []session.QuizCard{{Options: []string{"дом", "бежать", "учить", "дерево"}, CorrectIndex: 0}},
		Cursor: 0,
	}
	card := session.QuizCard{
		Lemma: "house", Direction: session.QuizForeignToNative,
		Options: []string{"дом", "бежать", "учить", "дерево"}, CorrectIndex: 0,
	}

	text := renderQuizFeedback(loc, snap, card, 1, false, false, nil)
	if !strings.Contains(text, "❌") {
		t.Errorf("wrong feedback must contain ❌, got: %q", text)
	}
}

func TestRenderQuizFeedback_ExampleInBlockquote(t *testing.T) {
	loc := ruLocalizer(t)
	snap := session.QuizSnapshot{
		Deck:   []session.QuizCard{{Options: []string{"дом"}, CorrectIndex: 0}},
		Cursor: 0,
	}
	card := session.QuizCard{
		Lemma: "house", Direction: session.QuizForeignToNative,
		Options:       []string{"дом", "x", "y", "z"},
		CorrectIndex:  0,
		ExampleTarget: "The house is big.",
		ExampleNative: "Дом большой.",
	}

	text := renderQuizFeedback(loc, snap, card, 0, true, false, nil)
	if !strings.Contains(text, "<blockquote>") {
		t.Errorf("example must be in <blockquote>, got: %q", text)
	}
}

func TestFilterSingleWord_RemovesPhrasesKeepsSingleWords(t *testing.T) {
	input := []string{"дом", "два слова", "бежать", "три слова тут", "учить"}
	got := filterSingleWord(input)
	want := []string{"дом", "бежать", "учить"}
	if len(got) != len(want) {
		t.Fatalf("filterSingleWord(%v) = %v, want %v", input, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: got %q want %q", i, got[i], want[i])
		}
	}
}

func TestFilterSingleWord_EmptyInputReturnsEmpty(t *testing.T) {
	if got := filterSingleWord(nil); len(got) != 0 {
		t.Fatalf("expected empty, got %v", got)
	}
}
