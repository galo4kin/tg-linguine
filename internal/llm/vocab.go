package llm

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"text/template"
)

//go:embed prompts/vocab_system.txt
var vocabSystemPrompt string

//go:embed prompts/vocab_user.tmpl
var vocabUserTemplateRaw string

//go:embed schema/vocab.json
var vocabSchemaRaw []byte

type ExtractVocabRequest struct {
	TargetLanguage     string
	NativeLanguage     string
	CEFR               string
	KnownWords         []string
	AlreadyFoundLemmas []string
	ArticleText        string
	VocabTarget        int
}

type ExtractVocabResponse struct {
	Words []AnalyzedWord `json:"words"`
}

var (
	vocabUserTmpl = template.Must(template.New("vocab-user").Parse(vocabUserTemplateRaw))
	vocabSchema   = mustCompileSchema(vocabSchemaRaw)
)

func VocabSystemPrompt() string { return vocabSystemPrompt }

func RenderVocabUserPrompt(req ExtractVocabRequest) (string, error) {
	var buf bytes.Buffer
	if err := vocabUserTmpl.Execute(&buf, req); err != nil {
		return "", fmt.Errorf("llm: render vocab prompt: %w", err)
	}
	return buf.String(), nil
}

func ValidateVocabJSON(raw []byte) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return fmt.Errorf("%w: %v", ErrSchemaInvalid, err)
	}
	if err := vocabSchema.Validate(v); err != nil {
		return fmt.Errorf("%w: %v", ErrSchemaInvalid, err)
	}
	return nil
}
