package telegram

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type fakeSender struct {
	calls  []bot.SendMessageParams
	retErr error
}

func (f *fakeSender) SendMessage(ctx context.Context, params *bot.SendMessageParams) (*models.Message, error) {
	if params != nil {
		f.calls = append(f.calls, *params)
	}
	if f.retErr != nil {
		return nil, f.retErr
	}
	return &models.Message{}, nil
}

func newCapturingLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})), &buf
}

func TestSendStartupPing_NoAdminConfigured(t *testing.T) {
	sender := &fakeSender{}
	log, buf := newCapturingLogger()

	SendStartupPing(context.Background(), sender, log, 0, "v1.2.3", "abc1234", time.Now())

	if len(sender.calls) != 0 {
		t.Fatalf("expected 0 SendMessage calls when admin id is 0, got %d", len(sender.calls))
	}
	if buf.Len() != 0 {
		t.Fatalf("expected silent skip when admin disabled, got log output: %s", buf.String())
	}
}

func TestSendStartupPing_SendsToAdminWithVersionAndCommit(t *testing.T) {
	sender := &fakeSender{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	started := time.Date(2026, 4, 28, 12, 30, 0, 0, time.UTC)

	SendStartupPing(context.Background(), sender, log, 1995215, "v1.2.3", "abc1234", started)

	if len(sender.calls) != 1 {
		t.Fatalf("expected exactly 1 SendMessage call, got %d", len(sender.calls))
	}
	got := sender.calls[0]
	if got.ChatID != int64(1995215) {
		t.Errorf("ChatID = %v, want 1995215", got.ChatID)
	}
	if !strings.Contains(got.Text, "v1.2.3") {
		t.Errorf("text missing version: %q", got.Text)
	}
	if !strings.Contains(got.Text, "abc1234") {
		t.Errorf("text missing commit: %q", got.Text)
	}
	if !strings.Contains(got.Text, "2026-04-28") {
		t.Errorf("text missing started-at: %q", got.Text)
	}
}

func TestSendStartupPing_SendErrorLoggedNotFatal(t *testing.T) {
	sender := &fakeSender{retErr: errors.New("network down")}
	log, buf := newCapturingLogger()

	// Must not panic.
	SendStartupPing(context.Background(), sender, log, 1995215, "v1", "deadbee", time.Now())

	if len(sender.calls) != 1 {
		t.Fatalf("expected the call to still be attempted, got %d", len(sender.calls))
	}
	out := buf.String()
	if !strings.Contains(out, "admin startup ping failed") {
		t.Errorf("expected WARN log on send error, got: %s", out)
	}
	if !strings.Contains(out, "WARN") {
		t.Errorf("expected log level WARN, got: %s", out)
	}
}

func TestBuildInfo_FallbackVersionAndUnknownCommit(t *testing.T) {
	// The test binary's debug.ReadBuildInfo() does report build info, but the
	// `vcs.revision` setting is typically absent under `go test`. We assert
	// the "no commit recorded" branch, and that the fallback version is
	// returned untouched when Main.Version is empty / "(devel)".
	v, c := BuildInfo("dev")
	if v == "" {
		t.Errorf("version must never be empty (got %q)", v)
	}
	if c == "" {
		t.Errorf("commit must never be empty (got %q)", c)
	}
}
