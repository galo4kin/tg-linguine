package screen_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"
	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	tgi18n "github.com/nikita/tg-linguine/internal/i18n"
	"github.com/nikita/tg-linguine/internal/screen"
)

// testLoc returns an English localizer backed by the real embedded locale files.
func testLoc(t *testing.T) *goi18n.Localizer {
	t.Helper()
	bundle := tgi18n.NewTestBundle()
	return tgi18n.For(bundle, "en")
}

func TestWithNavigation_NoParent_AddsHomeOnly(t *testing.T) {
	body := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{{Text: "X", CallbackData: "x"}},
		},
	}
	out := screen.WithNavigation(testLoc(t), body, "" /*parent*/, nil /*ctx*/)
	last := out.InlineKeyboard[len(out.InlineKeyboard)-1]
	if len(last) != 1 || !strings.Contains(last[0].Text, "Home") {
		t.Fatalf("expected only Home in last row, got %+v", last)
	}
	if last[0].CallbackData != screen.CallbackPrefixNav+"home" {
		t.Fatalf("unexpected Home callback: %s", last[0].CallbackData)
	}
}

func TestWithNavigation_WithParent_AddsBackAndHome(t *testing.T) {
	body := &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{}}
	ctx := map[string]string{"page": "3", "filter": "k"}
	out := screen.WithNavigation(testLoc(t), body, screen.ScreenMyWords, ctx)
	last := out.InlineKeyboard[len(out.InlineKeyboard)-1]
	if len(last) != 2 {
		t.Fatalf("expected Back+Home, got %+v", last)
	}
	if !strings.HasPrefix(last[0].CallbackData, screen.CallbackPrefixNav+"back:") {
		t.Fatalf("Back data: %s", last[0].CallbackData)
	}
	if last[1].CallbackData != screen.CallbackPrefixNav+"home" {
		t.Fatalf("Home data: %s", last[1].CallbackData)
	}
	// verify context survives round-trip in callback data
	backCD := strings.TrimPrefix(last[0].CallbackData, screen.CallbackPrefixNav+"back:")
	parts := strings.SplitN(backCD, ":", 2)
	if parts[0] != string(screen.ScreenMyWords) {
		t.Fatalf("parent: %s", parts[0])
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(parts[1]), &got); err != nil {
		t.Fatalf("ctx unmarshal: %v", err)
	}
	if got["page"] != "3" || got["filter"] != "k" {
		t.Fatalf("ctx mismatch: %+v", got)
	}
}

func TestWithNavigation_Welcome_NoNav(t *testing.T) {
	body := &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
		{{Text: "X", CallbackData: "x"}},
	}}
	out := screen.WithNavigationFor(testLoc(t), body, screen.ScreenWelcome, "", nil)
	if len(out.InlineKeyboard) != 1 {
		t.Fatalf("welcome should have no nav row: %+v", out)
	}
}

func TestWithNavigation_NilBody(t *testing.T) {
	// should not panic
	out := screen.WithNavigation(testLoc(t), nil, "", nil)
	if out == nil {
		t.Fatal("expected non-nil result")
	}
	if len(out.InlineKeyboard) != 1 {
		t.Fatalf("expected 1 row (Home), got %d", len(out.InlineKeyboard))
	}
}
