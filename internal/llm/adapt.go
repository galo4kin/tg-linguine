package llm

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"text/template"
)

//go:embed prompts/adapt_system.txt
var adaptSystemPrompt string

//go:embed prompts/adapt_user.tmpl
var adaptUserTemplateRaw string

//go:embed schema/adapt.json
var adaptSchemaRaw []byte

// AdaptRequest asks the LLM to rewrite an article body at a specific CEFR
// level. SourceText is the closest-available adaptation we already have on
// disk; SourceCEFR is the level it represents (may be empty if unknown).
type AdaptRequest struct {
	TargetLanguage string
	NativeLanguage string
	TargetCEFR     string
	SourceCEFR     string
	SourceText     string
}

// AdaptResponse is the mini-schema reply: the body rewritten at TargetCEFR
// and a 1-3 sentence summary in the target language at the same level.
type AdaptResponse struct {
	AdaptedText   string `json:"adapted_text"`
	SummaryTarget string `json:"summary_target"`
}

var (
	adaptUserTmpl = template.Must(template.New("adapt-user").Parse(adaptUserTemplateRaw))
	adaptSchema   = mustCompileSchema(adaptSchemaRaw)
)

// AdaptSystemPrompt returns the embedded English system prompt for the
// adapt-only mini-call.
func AdaptSystemPrompt() string { return adaptSystemPrompt }

// RenderAdaptUserPrompt fills the adapt-only user template with the request.
func RenderAdaptUserPrompt(req AdaptRequest) (string, error) {
	var buf bytes.Buffer
	if err := adaptUserTmpl.Execute(&buf, req); err != nil {
		return "", fmt.Errorf("llm: render adapt prompt: %w", err)
	}
	return buf.String(), nil
}

// ValidateAdaptJSON validates a raw JSON byte slice against the adapt schema.
// Returns ErrSchemaInvalid (wrapped) on any validation problem.
func ValidateAdaptJSON(raw []byte) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return fmt.Errorf("%w: %v", ErrSchemaInvalid, err)
	}
	if err := adaptSchema.Validate(v); err != nil {
		return fmt.Errorf("%w: %v", ErrSchemaInvalid, err)
	}
	return nil
}
