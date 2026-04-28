package handlers

import (
	"fmt"
	"strings"

	"github.com/go-telegram/bot/models"
	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/nikita/tg-linguine/internal/articles"
	tgi18n "github.com/nikita/tg-linguine/internal/i18n"
)

const articleCardPreviewLimit = 5

// renderArticleCard renders the headline + CEFR + summary + a short preview of
// the first few lemmas. previewLemmas is at most articleCardPreviewLimit long;
// totalWords is the actual count of stored words for that article (used to
// decide whether to show the "+N more" tail).
func renderArticleCard(loc *goi18n.Localizer, article *articles.Article, previewLemmas []string, totalWords int) string {
	if article == nil {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s\n", strings.TrimSpace(article.Title))
	if article.CEFRDetected != "" {
		fmt.Fprintf(&sb, "%s\n\n", tgi18n.T(loc, "article.card.cefr", map[string]string{"Level": article.CEFRDetected}))
	} else {
		sb.WriteString("\n")
	}
	if article.SummaryTarget != "" {
		sb.WriteString(strings.TrimSpace(article.SummaryTarget))
		sb.WriteString("\n\n")
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
// card. When totalWords == 0 there is nothing to paginate so the keyboard is
// nil.
func articleCardKeyboard(loc *goi18n.Localizer, articleID int64, totalWords int) *models.InlineKeyboardMarkup {
	if totalWords == 0 {
		return nil
	}
	return &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{{
				Text:         tgi18n.T(loc, "article.card.show_all_words", nil),
				CallbackData: fmt.Sprintf("%s%d:0", CallbackPrefixWords, articleID),
			}},
		},
	}
}
