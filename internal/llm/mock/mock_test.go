package mock_test

import (
	"context"
	"errors"
	"testing"

	"github.com/nikita/tg-linguine/internal/llm"
	"github.com/nikita/tg-linguine/internal/llm/mock"
)

func TestNew_DefaultsToCleanFixtures(t *testing.T) {
	p, err := mock.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	resp, err := p.Analyze(context.Background(), "k", llm.AnalyzeRequest{TargetLanguage: "en"})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(resp.Words) == 0 {
		t.Fatalf("expected fixture to contain words")
	}
	if len(resp.SafetyFlags) != 0 {
		t.Fatalf("clean fixture must have no safety flags, got %v", resp.SafetyFlags)
	}
	if len(p.AnalyzeCalls) != 1 || p.AnalyzeCalls[0].TargetLanguage != "en" {
		t.Fatalf("expected 1 recorded analyze call with target=en, got %+v", p.AnalyzeCalls)
	}
}

func TestLoadAnalyze_FlaggedFixturePassesSchema(t *testing.T) {
	resp, err := mock.LoadAnalyze("analyze_flagged")
	if err != nil {
		t.Fatalf("LoadAnalyze: %v", err)
	}
	if len(resp.SafetyFlags) == 0 {
		t.Fatalf("flagged fixture must surface safety flags")
	}
}

func TestProvider_AnalyzeErrorPath(t *testing.T) {
	p, err := mock.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	p.AnalyzeErr = llm.ErrRateLimited
	_, err = p.Analyze(context.Background(), "k", llm.AnalyzeRequest{})
	if !errors.Is(err, llm.ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}
}

func TestProvider_ValidateAPIKey(t *testing.T) {
	p, _ := mock.New()
	if err := p.ValidateAPIKey(context.Background(), "anything"); err != nil {
		t.Fatalf("default ValidateAPIKey should succeed, got %v", err)
	}
	p.ValidateErr = llm.ErrInvalidAPIKey
	if err := p.ValidateAPIKey(context.Background(), "anything"); !errors.Is(err, llm.ErrInvalidAPIKey) {
		t.Fatalf("expected ErrInvalidAPIKey, got %v", err)
	}
}

func TestLoadAnalyze_MissingFixture(t *testing.T) {
	_, err := mock.LoadAnalyze("does_not_exist")
	if err == nil {
		t.Fatalf("expected error for missing fixture")
	}
}
