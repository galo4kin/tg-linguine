package llm

import (
	"bytes"
	_ "embed"
	"fmt"
	"text/template"
)

//go:embed prompts/summarize_system.txt
var summarizeSystemPrompt string

//go:embed prompts/summarize_user.tmpl
var summarizeUserTemplateRaw string

var summarizeUserTmpl = template.Must(template.New("summarize_user").Parse(summarizeUserTemplateRaw))

// SummarizeRequest is the input to the long-article pre-summary call. The
// request asks the model to compress ArticleText down to roughly
// TargetTokens tokens, *staying in the article's original language* so the
// downstream analysis still has authentic vocabulary to extract.
type SummarizeRequest struct {
	TargetLanguage string
	ArticleTitle   string
	ArticleText    string
	TargetTokens   int
}

// SummarizeSystemPrompt returns the embedded system prompt for the
// summarize fallback.
func SummarizeSystemPrompt() string { return summarizeSystemPrompt }

// RenderSummarizeUserPrompt fills the user template with the request fields.
func RenderSummarizeUserPrompt(req SummarizeRequest) (string, error) {
	var buf bytes.Buffer
	if err := summarizeUserTmpl.Execute(&buf, req); err != nil {
		return "", fmt.Errorf("llm: render summarize prompt: %w", err)
	}
	return buf.String(), nil
}
