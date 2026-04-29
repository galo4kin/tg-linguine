// Package mock is a JSON-fixture-driven implementation of llm.Provider for
// tests and local experimentation. It does not contact any network endpoint.
//
// The mock loads canned `analysis.json` / `adapt.json` shapes from the
// embedded `fixtures/` directory. Tests pick a fixture by name, optionally
// override individual fields, and inject the result into the article
// pipeline. This gives us a deterministic LLM stand-in that exercises the
// real schema without burning Groq tokens.
package mock

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"

	"github.com/nikita/tg-linguine/internal/llm"
)

//go:embed fixtures/*.json
var fixturesFS embed.FS

// Provider is an in-memory llm.Provider replacement. Configure the canned
// behaviour at construction time; assertions run against the recorded
// AnalyzeRequests/AdaptRequests after the call.
type Provider struct {
	// AnalyzeResp is returned from Analyze. AnalyzeErr (if non-nil) is
	// returned instead.
	AnalyzeResp llm.AnalyzeResponse
	AnalyzeErr  error

	// AdaptResp / AdaptErr are the same for Adapt.
	AdaptResp llm.AdaptResponse
	AdaptErr  error

	// SummarizeResp / SummarizeErr drive the long-article pre-summary
	// fallback. The default empty string is fine for tests that never hit
	// the summarize path.
	SummarizeResp string
	SummarizeErr  error

	// ExtractVocabResp / ExtractVocabErr drive the vocab-only chunk
	// extraction pass for long articles.
	ExtractVocabResp llm.ExtractVocabResponse
	ExtractVocabErr  error

	// ValidateErr is returned by ValidateAPIKey. Nil means the key is
	// accepted as valid.
	ValidateErr error

	// Calls records every invocation in order so tests can assert on the
	// fields the article pipeline forwarded (KnownWords, target language,
	// etc.) without exposing the underlying loop.
	AnalyzeCalls      []llm.AnalyzeRequest
	AdaptCalls        []llm.AdaptRequest
	SummarizeCalls    []llm.SummarizeRequest
	ExtractVocabCalls []llm.ExtractVocabRequest
}

// LoadAnalyze reads `fixtures/<name>.json`, validates it against the live
// analysis schema, and returns the decoded AnalyzeResponse. The validation
// step is the point — it guarantees the mock cannot drift from the real
// llm.AnalyzeResponse contract.
func LoadAnalyze(name string) (llm.AnalyzeResponse, error) {
	raw, err := fixturesFS.ReadFile("fixtures/" + name + ".json")
	if err != nil {
		return llm.AnalyzeResponse{}, fmt.Errorf("mock: read fixture %q: %w", name, err)
	}
	if err := llm.ValidateAnalysisJSON(raw); err != nil {
		return llm.AnalyzeResponse{}, fmt.Errorf("mock: fixture %q: %w", name, err)
	}
	var resp llm.AnalyzeResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return llm.AnalyzeResponse{}, fmt.Errorf("mock: unmarshal fixture %q: %w", name, err)
	}
	return resp, nil
}

// LoadAdapt reads `fixtures/<name>.json` and decodes it as an AdaptResponse.
// There is no schema for Adapt, so this is a plain unmarshal — empty fields
// fall through as you'd expect.
func LoadAdapt(name string) (llm.AdaptResponse, error) {
	raw, err := fixturesFS.ReadFile("fixtures/" + name + ".json")
	if err != nil {
		return llm.AdaptResponse{}, fmt.Errorf("mock: read fixture %q: %w", name, err)
	}
	var resp llm.AdaptResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return llm.AdaptResponse{}, fmt.Errorf("mock: unmarshal fixture %q: %w", name, err)
	}
	return resp, nil
}

// New returns a Provider preloaded with the `analyze_clean` / `adapt_clean`
// fixtures — the most common starting point for tests. Override fields on
// the returned struct to switch behaviours.
func New() (*Provider, error) {
	analyze, err := LoadAnalyze("analyze_clean")
	if err != nil {
		return nil, err
	}
	adapt, err := LoadAdapt("adapt_clean")
	if err != nil {
		return nil, err
	}
	return &Provider{
		AnalyzeResp: analyze,
		AdaptResp:   adapt,
	}, nil
}

func (p *Provider) ValidateAPIKey(ctx context.Context, key string) error {
	return p.ValidateErr
}

func (p *Provider) Analyze(ctx context.Context, key string, req llm.AnalyzeRequest) (llm.AnalyzeResponse, error) {
	p.AnalyzeCalls = append(p.AnalyzeCalls, req)
	if p.AnalyzeErr != nil {
		return llm.AnalyzeResponse{}, p.AnalyzeErr
	}
	return p.AnalyzeResp, nil
}

func (p *Provider) Adapt(ctx context.Context, key string, req llm.AdaptRequest) (llm.AdaptResponse, error) {
	p.AdaptCalls = append(p.AdaptCalls, req)
	if p.AdaptErr != nil {
		return llm.AdaptResponse{}, p.AdaptErr
	}
	return p.AdaptResp, nil
}

func (p *Provider) Summarize(ctx context.Context, key string, req llm.SummarizeRequest) (string, error) {
	p.SummarizeCalls = append(p.SummarizeCalls, req)
	if p.SummarizeErr != nil {
		return "", p.SummarizeErr
	}
	return p.SummarizeResp, nil
}

func (p *Provider) ExtractVocab(ctx context.Context, key string, req llm.ExtractVocabRequest) (llm.ExtractVocabResponse, error) {
	p.ExtractVocabCalls = append(p.ExtractVocabCalls, req)
	if p.ExtractVocabErr != nil {
		return llm.ExtractVocabResponse{}, p.ExtractVocabErr
	}
	return p.ExtractVocabResp, nil
}

// Compile-time guarantee that Provider satisfies the llm.Provider contract.
var _ llm.Provider = (*Provider)(nil)
