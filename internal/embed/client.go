// Package embed provides a minimal HTTP client for interacting with the Ollama
// embeddings API.  It is designed to be used by the TUI config view for
// connection testing and by the (future) embeddings engine for inference.
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is a lightweight Ollama API client.  Zero-value is not useful;
// construct via a struct literal or the DefaultClient helper.
//
// BaseURL should be the scheme+host+port with no trailing slash
// (e.g. "http://localhost:11434").
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// defaultHTTPTimeout is the HTTP client timeout used by DefaultClient. It is
// intentionally generous (60s) because embedding requests over large batches
// of text can be slow on CPU-only Ollama installs; callers that need a
// tighter bound should control it per-request via context.WithTimeout.
const defaultHTTPTimeout = 60 * time.Second

// DefaultClient returns a Client pointed at baseURL with a 60-second HTTP
// client timeout. Callers still control per-request deadlines via ctx.
func DefaultClient(baseURL string) *Client {
	return &Client{
		BaseURL: baseURL,
		HTTP:    &http.Client{Timeout: defaultHTTPTimeout},
	}
}

// maxStatusErrorBodySnippet caps how much of a non-200 response body is read
// and echoed back in the error message produced by statusError.
const maxStatusErrorBodySnippet = 512

// statusError builds the error returned for a non-200 HTTP response. It reads
// up to maxStatusErrorBodySnippet bytes of resp.Body and includes a trimmed,
// single-line snippet in the message, e.g.:
//
//	embed: unexpected status 500: {"error":"model not found"}
//
// When the body is empty (or only whitespace), the snippet is omitted and the
// error is just "embed: unexpected status <code>".
func statusError(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxStatusErrorBodySnippet))
	// Drain the remainder so the transport can reuse the connection.
	_, _ = io.Copy(io.Discard, resp.Body)
	snippet := strings.Join(strings.Fields(string(raw)), " ")
	if snippet == "" {
		return fmt.Errorf("embed: unexpected status %d", resp.StatusCode)
	}
	return fmt.Errorf("embed: unexpected status %d: %s", resp.StatusCode, snippet)
}

// Ping checks that the Ollama server is reachable by calling GET /api/tags.
// A non-200 HTTP status or a network error is returned as an error.
func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint("/api/tags"), nil)
	if err != nil {
		return fmt.Errorf("embed: building ping request: %w", err)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("embed: ping request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return statusError(resp)
	}
	return nil
}

// tagsResponse is the JSON shape returned by GET /api/tags.
type tagsResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

// HasModel checks whether model is present in the Ollama server's model list.
// The comparison strips the ":latest" suffix so that "nomic-embed-text" matches
// "nomic-embed-text:latest".
func (c *Client) HasModel(ctx context.Context, model string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint("/api/tags"), nil)
	if err != nil {
		return false, fmt.Errorf("embed: building has-model request: %w", err)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return false, fmt.Errorf("embed: has-model request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, statusError(resp)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("embed: reading has-model response: %w", err)
	}

	var tags tagsResponse
	if err := json.Unmarshal(body, &tags); err != nil {
		return false, fmt.Errorf("embed: decoding has-model response: %w", err)
	}

	want := strings.TrimSuffix(model, ":latest")
	for _, m := range tags.Models {
		name := strings.TrimSuffix(m.Name, ":latest")
		if name == want {
			return true, nil
		}
	}
	return false, nil
}

// embedRequest is the JSON body sent to POST /api/embeddings.
type embedRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

// embedResponse is the relevant part of the JSON response from /api/embeddings.
type embedResponse struct {
	Embedding []float32 `json:"embedding"`
}

// ProbeEmbed sends a minimal embedding request to verify that the model is
// functional.  It returns the dimension count of the resulting embedding and
// the round-trip latency.  An error is returned for network failures,
// non-200 status codes, or malformed JSON.
func (c *Client) ProbeEmbed(ctx context.Context, model string) (dims int, elapsed time.Duration, err error) {
	reqBody, err := json.Marshal(embedRequest{Model: model, Prompt: "ping"})
	if err != nil {
		return 0, 0, fmt.Errorf("embed: encoding probe request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("/api/embeddings"),
		bytes.NewReader(reqBody))
	if err != nil {
		return 0, 0, fmt.Errorf("embed: building probe request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := c.HTTP.Do(req)
	elapsed = time.Since(start)
	if err != nil {
		return 0, elapsed, fmt.Errorf("embed: probe request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, elapsed, statusError(resp)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, elapsed, fmt.Errorf("embed: reading probe response: %w", err)
	}

	var er embedResponse
	if err := json.Unmarshal(body, &er); err != nil {
		return 0, elapsed, fmt.Errorf("embed: decoding probe response: %w", err)
	}

	return len(er.Embedding), elapsed, nil
}

// endpoint joins path onto BaseURL, tolerating a trailing slash in the
// user-configured base (e.g. "http://localhost:11434/").
func (c *Client) endpoint(path string) string {
	return strings.TrimRight(c.BaseURL, "/") + path
}
