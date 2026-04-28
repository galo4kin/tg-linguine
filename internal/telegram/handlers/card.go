package handlers

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/go-telegram/bot/models"
	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/nikita/tg-linguine/internal/articles"
	tgi18n "github.com/nikita/tg-linguine/internal/i18n"
	"github.com/nikita/tg-linguine/internal/users"
)

const articleCardPreviewLimit = 5

// CallbackPrefixCard drives article-card view switches: changing the
// adapted-version level or toggling the summary language. Payload shape is
// `art:v:<article_id>:<lvl>:<sum>` with lvl ∈ {l,c,h} and sum ∈ {t,n}.
const CallbackPrefixCard = "art:"

// ArticleView is the per-card UI state for the inline view switches. It is
// encoded into callback data on every button so we don't need any
// server-side session storage to track which adaptation/summary the user
// last picked.
type ArticleView struct {
	Level         CardLevel
	SummaryNative bool
}

// DefaultCardView is the state used the first time the user sees the card —
// adapted body at the user's CEFR level, summary in the target language.
func DefaultCardView() ArticleView {
	return ArticleView{Level: CardLevelCurrent}
}

type CardLevel string

const (
	CardLevelLower   CardLevel = "lower"
	CardLevelCurrent CardLevel = "current"
	CardLevelHigher  CardLevel = "higher"
)

func (l CardLevel) short() string {
	switch l {
	case CardLevelLower:
		return "l"
	case CardLevelHigher:
		return "h"
	default:
		return "c"
	}
}

func parseCardLevel(s string) CardLevel {
	switch s {
	case "l":
		return CardLevelLower
	case "h":
		return CardLevelHigher
	default:
		return CardLevelCurrent
	}
}

// resolveAbsoluteLevel maps a relative card view to an absolute CEFR level
// using the user's current learning level. Returns ("", false) when the
// shift falls off the A1..C2 range (no "lower" at A1, no "higher" at C2).
func resolveAbsoluteLevel(userCEFR string, view CardLevel) (string, bool) {
	switch view {
	case CardLevelLower:
		return users.CEFRShift(userCEFR, -1)
	case CardLevelHigher:
		return users.CEFRShift(userCEFR, +1)
	default:
		if users.IsCEFR(userCEFR) {
			return userCEFR, true
		}
		return "", false
	}
}

func levelLabelKey(view CardLevel) string {
	switch view {
	case CardLevelLower:
		return "article.card.adapted.lower"
	case CardLevelHigher:
		return "article.card.adapted.higher"
	default:
		return "article.card.adapted.current"
	}
}

// renderArticleCard renders the headline, the (target/native) summary, the
// adapted body for the chosen level (when present), and a short preview of
// the first few lemmas.
func renderArticleCard(loc *goi18n.Localizer, article *articles.Article, userCEFR string, previewLemmas []string, totalWords int, view ArticleView) string {
	if article == nil {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s\n", strings.TrimSpace(article.Title))
	if article.CEFRDetected != "" {
		fmt.Fprintf(&sb, "%s\n", tgi18n.T(loc, "article.card.cefr", map[string]string{"Level": article.CEFRDetected}))
	}
	sb.WriteString("\n")

	summaryText := article.SummaryTarget
	summaryLabelKey := "article.card.summary.target"
	if view.SummaryNative && article.SummaryNative != "" {
		summaryText = article.SummaryNative
		summaryLabelKey = "article.card.summary.native"
	}
	if summaryText != "" {
		fmt.Fprintf(&sb, "%s\n%s\n\n", tgi18n.T(loc, summaryLabelKey, nil), strings.TrimSpace(summaryText))
	}

	adapted := article.ParseAdaptedVersions()
	if abs, ok := resolveAbsoluteLevel(userCEFR, view.Level); ok {
		if body := adapted[abs]; body != "" {
			fmt.Fprintf(&sb, "%s\n%s\n\n", tgi18n.T(loc, levelLabelKey(view.Level), nil), strings.TrimSpace(body))
		}
	}

	if totalWords == 0 {
		sb.WriteString(tgi18n.T(loc, "article.card.no_words", nil))
		return sb.String()
	}
	sb.WriteString(tgi18n.T(loc, "article.card.words_header", nil))
	sb.WriteString("\n")
	limit := len(previewLemmas)
	if limit > articleCardPreviewLimit {
		limit = articleCardPreviewLimit
	}
	for i := 0; i < limit; i++ {
		fmt.Fprintf(&sb, "• %s\n", previewLemmas[i])
	}
	if totalWords > articleCardPreviewLimit {
		sb.WriteString(tgi18n.T(loc, "article.card.more_words", map[string]int{"Count": totalWords - articleCardPreviewLimit}))
	}
	return sb.String()
}

// articleCardKeyboard returns the inline keyboard shown under the article
// card. It always renders three "level" buttons (greying out unavailable
// adaptations with a noop callback), a summary-language toggle when both
// summaries exist, and the "Show all words" button when totalWords > 0.
func articleCardKeyboard(loc *goi18n.Localizer, article *articles.Article, userCEFR string, totalWords int, view ArticleView) *models.InlineKeyboardMarkup {
	if article == nil {
		return nil
	}
	rows := make([][]models.InlineKeyboardButton, 0, 3)

	if users.IsCEFR(userCEFR) {
		rows = append(rows, levelRow(loc, article.ID, view, userCEFR))
	}

	if article.SummaryTarget != "" && article.SummaryNative != "" {
		rows = append(rows, []models.InlineKeyboardButton{summaryToggleButton(loc, article.ID, view)})
	}

	if totalWords > 0 {
		rows = append(rows, []models.InlineKeyboardButton{{
			Text:         tgi18n.T(loc, "article.card.show_all_words", nil),
			CallbackData: fmt.Sprintf("%s%d:0", CallbackPrefixWords, article.ID),
		}})
	}

	if len(rows) == 0 {
		return nil
	}
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func levelRow(loc *goi18n.Localizer, articleID int64, view ArticleView, userCEFR string) []models.InlineKeyboardButton {
	return []models.InlineKeyboardButton{
		levelButton(loc, articleID, view, CardLevelLower, "article.card.btn.level_lower", userCEFR),
		levelButton(loc, articleID, view, CardLevelCurrent, "article.card.btn.level_current", userCEFR),
		levelButton(loc, articleID, view, CardLevelHigher, "article.card.btn.level_higher", userCEFR),
	}
}

func levelButton(loc *goi18n.Localizer, articleID int64, view ArticleView, target CardLevel, labelKey, userCEFR string) models.InlineKeyboardButton {
	label := tgi18n.T(loc, labelKey, nil)
	if view.Level == target {
		label = "✓ " + label
	}
	_, available := resolveAbsoluteLevel(userCEFR, target)
	if !available {
		return models.InlineKeyboardButton{
			Text:         label,
			CallbackData: CallbackPrefixCard + "noop",
		}
	}
	return models.InlineKeyboardButton{
		Text:         label,
		CallbackData: cardCallback(articleID, ArticleView{Level: target, SummaryNative: view.SummaryNative}),
	}
}

func summaryToggleButton(loc *goi18n.Localizer, articleID int64, view ArticleView) models.InlineKeyboardButton {
	// The button label advertises the OTHER language — "Перевод summary" goes
	// to native, "Original summary" goes back to target.
	var labelKey string
	next := view
	if view.SummaryNative {
		labelKey = "article.card.btn.summary_target"
		next.SummaryNative = false
	} else {
		labelKey = "article.card.btn.summary_native"
		next.SummaryNative = true
	}
	return models.InlineKeyboardButton{
		Text:         tgi18n.T(loc, labelKey, nil),
		CallbackData: cardCallback(articleID, next),
	}
}

func cardCallback(articleID int64, view ArticleView) string {
	sum := "t"
	if view.SummaryNative {
		sum = "n"
	}
	return fmt.Sprintf("%sv:%d:%s:%s", CallbackPrefixCard, articleID, view.Level.short(), sum)
}

// parseCardCallback decodes the suffix after `art:` — expected shape is
// `v:<article_id>:<lvl>:<sum>`.
func parseCardCallback(payload string) (articleID int64, view ArticleView, ok bool) {
	parts := strings.Split(payload, ":")
	if len(parts) != 4 || parts[0] != "v" {
		return 0, ArticleView{}, false
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, ArticleView{}, false
	}
	view.Level = parseCardLevel(parts[2])
	view.SummaryNative = parts[3] == "n"
	return id, view, true
}
