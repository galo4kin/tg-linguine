package llm

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"text/template"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

//go:embed prompts/system.txt
var systemPrompt string

//go:embed prompts/user.tmpl
var userTemplateRaw string

//go:embed schema/analysis.json
var analysisSchemaRaw []byte

// AnalyzeRequest is the input to LLM article analysis.
type AnalyzeRequest struct {
	TargetLanguage string
	NativeLanguage string
	CEFR           string
	KnownWords     []string
	ArticleTitle   string
	ArticleText    string
}

type AdaptedVersions struct {
	Lower   string `json:"lower"`
	Current string `json:"current"`
	Higher  string `json:"higher"`
}

type AnalyzedWord struct {
	SurfaceForm       string `json:"surface_form"`
	Lemma             string `json:"lemma"`
	POS               string `json:"pos"`
	TranscriptionIPA  string `json:"transcription_ipa"`
	TranslationNative string `json:"translation_native"`
	ExampleTarget     string `json:"example_target"`
	ExampleNative     string `json:"example_native"`
}

type AnalyzeResponse struct {
	SummaryTarget   string          `json:"summary_target"`
	SummaryNative   string          `json:"summary_native"`
	Category        string          `json:"category"`
	CEFRDetected    string          `json:"cefr_detected"`
	AdaptedVersions AdaptedVersions `json:"adapted_versions"`
	Words           []AnalyzedWord  `json:"words"`
	SafetyFlags     []string        `json:"safety_flags"`
}

// ErrSchemaInvalid is returned when the LLM produced JSON that does not match the
// analysis schema (or is not valid JSON at all).
var ErrSchemaInvalid = errors.New("llm: analysis response failed schema validation")

var (
	userTmpl       = template.Must(template.New("user").Parse(userTemplateRaw))
	analysisSchema = mustCompileSchema(analysisSchemaRaw)
)

func mustCompileSchema(raw []byte) *jsonschema.Schema {
	c := jsonschema.NewCompiler()
	if err := c.AddResource("analysis.json", bytes.NewReader(raw)); err != nil {
		panic(fmt.Errorf("llm: add schema resource: %w", err))
	}
	s, err := c.Compile("analysis.json")
	if err != nil {
		panic(fmt.Errorf("llm: compile schema: %w", err))
	}
	return s
}

// SystemPrompt returns the embedded English system prompt.
func SystemPrompt() string { return systemPrompt }

// RenderUserPrompt fills the user template with the request fields.
func RenderUserPrompt(req AnalyzeRequest) (string, error) {
	var buf bytes.Buffer
	if err := userTmpl.Execute(&buf, req); err != nil {
		return "", fmt.Errorf("llm: render user prompt: %w", err)
	}
	return buf.String(), nil
}

// ValidateAnalysisJSON validates a raw JSON byte slice against the analysis schema.
// Returns ErrSchemaInvalid (wrapped) on any validation problem; the wrapped error
// carries the underlying jsonschema or json error for the retry message.
func ValidateAnalysisJSON(raw []byte) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return fmt.Errorf("%w: %v", ErrSchemaInvalid, err)
	}
	if err := analysisSchema.Validate(v); err != nil {
		return fmt.Errorf("%w: %v", ErrSchemaInvalid, err)
	}
	return nil
}

// EstimateTokens is a coarse upper-bound heuristic: ~1 token per 4 chars.
// Used for rough preflight checks; precise counters are out of scope here.
func EstimateTokens(s string) int {
	if s == "" {
		return 0
	}
	n := len([]rune(s))
	return (n + 3) / 4
}

// errorMessage extracts a one-line description suitable for asking the LLM to retry.
func RetryMessage(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	msg = strings.ReplaceAll(msg, "\n", " ")
	msg = strings.TrimSpace(msg)
	if len(msg) > 500 {
		msg = msg[:500]
	}
	return msg
}
