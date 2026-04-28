package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/goppydae/gollm/internal/types"
)

func TestOpenAIProvider_Stream_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\" world\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	p := NewOpenAIProvider(srv.URL, "gpt-test")
	ch, err := p.Stream(context.Background(), &CompletionRequest{
		Model:    "gpt-test",
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}

	var got string
	for ev := range ch {
		if ev.Type == EventTextDelta {
			got += ev.Content
		}
	}
	if got != "hello world" {
		t.Errorf("expected %q, got %q", "hello world", got)
	}
}

func TestOllamaProvider_Stream_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "{\"message\":{\"content\":\"hello\"},\"done\":false}\n{\"message\":{\"content\":\" world\"},\"done\":true}\n")
	}))
	defer srv.Close()

	p := NewOllamaProvider(srv.URL, "llama-test")
	ch, err := p.Stream(context.Background(), &CompletionRequest{
		Model:    "llama-test",
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}

	var got string
	for ev := range ch {
		if ev.Type == EventTextDelta {
			got += ev.Content
		}
	}
	if got != "hello world" {
		t.Errorf("expected %q, got %q", "hello world", got)
	}
}

func TestGoogleProvider_Stream_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "[{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hello\"}]}}]}, {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\" world\"}]}}]}]")
	}))
	defer srv.Close()

	p := NewGoogleProvider("test-key", "gemini-test").WithBaseURL(srv.URL)
	ch, err := p.Stream(context.Background(), &CompletionRequest{
		Model:    "gemini-test",
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}

	var got string
	for ev := range ch {
		if ev.Type == EventTextDelta {
			got += ev.Content
		}
	}
	if got != "hello world" {
		t.Errorf("expected %q, got %q", "hello world", got)
	}
}

func TestLlamaCppProvider_Stream_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\" world\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	p := NewLlamaCppProvider(srv.URL)
	ch, err := p.Stream(context.Background(), &CompletionRequest{
		Model:    "llamacpp",
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}

	var got string
	for ev := range ch {
		if ev.Type == EventTextDelta {
			got += ev.Content
		}
	}
	if got != "hello world" {
		t.Errorf("expected %q, got %q", "hello world", got)
	}
}
