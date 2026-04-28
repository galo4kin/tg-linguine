package translation_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nikita/tg-linguine/internal/translation"
)

func yandexResponse(translations []string) translation.YandexResponse {
	trs := make([]translation.YandexTr, len(translations))
	for i, t := range translations {
		trs[i] = translation.YandexTr{Text: t}
	}
	return translation.YandexResponse{
		Def: []translation.YandexDef{{
			Tr: trs,
		}},
	}
}

func TestYandexClient_Translate_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("text") != "compelling" {
			t.Errorf("unexpected text param: %s", r.URL.Query().Get("text"))
		}
		if r.URL.Query().Get("lang") != "en-ru" {
			t.Errorf("unexpected lang param: %s", r.URL.Query().Get("lang"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(yandexResponse([]string{"убедительный", "неотразимый", "принудительный"}))
	}))
	defer srv.Close()

	client := translation.NewYandex("test-key", translation.WithBaseURL(srv.URL))
	got, err := client.Translate(context.Background(), "compelling", "en", "ru")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "убедительный, неотразимый, принудительный"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestYandexClient_Translate_EmptyDef(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(translation.YandexResponse{})
	}))
	defer srv.Close()

	client := translation.NewYandex("test-key", translation.WithBaseURL(srv.URL))
	got, err := client.Translate(context.Background(), "xyzzy", "en", "ru")
	if err != nil {
		t.Fatalf("unexpected error on empty result: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestYandexClient_Translate_TrimsToThree(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(yandexResponse([]string{"a", "b", "c", "d", "e"}))
	}))
	defer srv.Close()

	client := translation.NewYandex("test-key", translation.WithBaseURL(srv.URL))
	got, err := client.Translate(context.Background(), "word", "en", "ru")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "a, b, c"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
