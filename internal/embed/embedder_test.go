package embed_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ionix/ion-mem/internal/embed"
)

// ─── Embed method ────────────────────────────────────────────────────────────

func TestEmbed_Success(t *testing.T) {
	want := []float32{0.1, 0.2, 0.3}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/embeddings" {
			http.NotFound(w, r)
			return
		}
		resp := map[string]any{"embedding": want}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := &embed.Client{BaseURL: srv.URL, HTTP: &http.Client{Timeout: 3 * time.Second}}
	got, err := c.Embed(context.Background(), "test text")
	if err != nil {
		t.Fatalf("Embed: unexpected error: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("Embed: got %d dims, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Embed: got[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestEmbed_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := &embed.Client{BaseURL: srv.URL, HTTP: &http.Client{Timeout: 3 * time.Second}}
	_, err := c.Embed(context.Background(), "test")
	if err == nil {
		t.Error("Embed non-200: expected error, got nil")
	}
}

func TestEmbed_EmptyEmbeddingIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{"embedding": []float32{}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := &embed.Client{BaseURL: srv.URL, HTTP: &http.Client{Timeout: 3 * time.Second}}
	_, err := c.Embed(context.Background(), "test")
	if err == nil {
		t.Error("Embed empty embedding: expected error, got nil")
	}
}

func TestEmbed_NetworkRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	c := &embed.Client{BaseURL: url, HTTP: &http.Client{Timeout: 200 * time.Millisecond}}
	_, err := c.Embed(context.Background(), "test")
	if err == nil {
		t.Error("Embed closed server: expected error, got nil")
	}
}

// ─── Embedder interface via OllamaEmbedder ───────────────────────────────────

func TestOllamaEmbedder_ImplementsEmbedder(t *testing.T) {
	// Verify OllamaEmbedder satisfies the Embedder interface at compile time.
	// If OllamaEmbedder doesn't implement Embedder, this will not compile.
	want := []float32{1.0, 2.0}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{"embedding": want}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := &embed.Client{BaseURL: srv.URL, HTTP: &http.Client{Timeout: 3 * time.Second}}
	var e embed.Embedder = embed.NewOllamaEmbedder(c, "nomic-embed-text")
	if e.Model() != "nomic-embed-text" {
		t.Errorf("OllamaEmbedder.Model() = %q, want %q", e.Model(), "nomic-embed-text")
	}

	got, err := e.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("OllamaEmbedder.Embed: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("OllamaEmbedder.Embed: got %d dims, want 2", len(got))
	}
}
