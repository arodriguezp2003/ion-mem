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

// ─── helpers ─────────────────────────────────────────────────────────────────

// tagsResponse builds the JSON body that /api/tags returns.
type tagsResponseBody struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

func jsonBody(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func newClient(baseURL string) *embed.Client {
	return &embed.Client{
		BaseURL: baseURL,
		HTTP:    &http.Client{Timeout: 3 * time.Second},
	}
}

// ─── Ping ────────────────────────────────────────────────────────────────────

func TestPing_HealthyServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(jsonBody(tagsResponseBody{}))
	}))
	defer srv.Close()

	c := newClient(srv.URL)
	if err := c.Ping(context.Background()); err != nil {
		t.Errorf("Ping healthy server: unexpected error: %v", err)
	}
}

func TestPing_ServerReturns500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newClient(srv.URL)
	err := c.Ping(context.Background())
	if err == nil {
		t.Error("Ping 500 response: expected error, got nil")
	}
}

func TestPing_ConnectionRefused(t *testing.T) {
	// Create and immediately close a server so its port is unbound.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	c := &embed.Client{
		BaseURL: url,
		HTTP:    &http.Client{Timeout: 500 * time.Millisecond},
	}
	err := c.Ping(context.Background())
	if err == nil {
		t.Error("Ping closed server: expected connection error, got nil")
	}
}

// ─── HasModel ────────────────────────────────────────────────────────────────

func TestHasModel_ModelPresent(t *testing.T) {
	body := tagsResponseBody{}
	body.Models = []struct {
		Name string `json:"name"`
	}{
		{Name: "nomic-embed-text:latest"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(jsonBody(body))
	}))
	defer srv.Close()

	c := newClient(srv.URL)
	ok, err := c.HasModel(context.Background(), "nomic-embed-text")
	if err != nil {
		t.Fatalf("HasModel: unexpected error: %v", err)
	}
	if !ok {
		t.Error("HasModel: expected true for model that is present (name match without :latest suffix)")
	}
}

func TestHasModel_ModelAbsent(t *testing.T) {
	body := tagsResponseBody{}
	body.Models = []struct {
		Name string `json:"name"`
	}{
		{Name: "llama3:latest"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(jsonBody(body))
	}))
	defer srv.Close()

	c := newClient(srv.URL)
	ok, err := c.HasModel(context.Background(), "nomic-embed-text")
	if err != nil {
		t.Fatalf("HasModel: unexpected error: %v", err)
	}
	if ok {
		t.Error("HasModel: expected false for model that is not in the list")
	}
}

func TestHasModel_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not-json"))
	}))
	defer srv.Close()

	c := newClient(srv.URL)
	_, err := c.HasModel(context.Background(), "nomic-embed-text")
	if err == nil {
		t.Error("HasModel malformed JSON: expected error, got nil")
	}
}

// ─── ProbeEmbed ───────────────────────────────────────────────────────────────

func TestProbeEmbed_Success(t *testing.T) {
	const dims = 768
	respBody := map[string]any{
		"embedding": make([]float32, dims),
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embeddings" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(jsonBody(respBody))
	}))
	defer srv.Close()

	c := newClient(srv.URL)
	gotDims, elapsed, err := c.ProbeEmbed(context.Background(), "nomic-embed-text")
	if err != nil {
		t.Fatalf("ProbeEmbed: unexpected error: %v", err)
	}
	if gotDims != dims {
		t.Errorf("ProbeEmbed dims = %d, want %d", gotDims, dims)
	}
	if elapsed < 0 {
		t.Errorf("ProbeEmbed elapsed = %v, should be >= 0", elapsed)
	}
}

func TestProbeEmbed_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newClient(srv.URL)
	_, _, err := c.ProbeEmbed(context.Background(), "nomic-embed-text")
	if err == nil {
		t.Error("ProbeEmbed 500: expected error, got nil")
	}
}

func TestProbeEmbed_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{bad json"))
	}))
	defer srv.Close()

	c := newClient(srv.URL)
	_, _, err := c.ProbeEmbed(context.Background(), "bad-model")
	if err == nil {
		t.Error("ProbeEmbed malformed JSON: expected error, got nil")
	}
}
