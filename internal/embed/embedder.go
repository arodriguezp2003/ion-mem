package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Embedder is the abstraction for text-to-vector embedding. It is implemented
// by OllamaEmbedder and can be replaced by any fake in tests.
//
// Embed converts text into a dense float32 vector. The caller is responsible
// for passing a reasonably-sized context (e.g. 3s timeout).
//
// Model returns the model name used by this embedder, e.g. "nomic-embed-text".
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	Model() string
}

// OllamaEmbedder implements Embedder using a Client talking to the Ollama API.
type OllamaEmbedder struct {
	client    *Client
	modelName string
}

// NewOllamaEmbedder creates an OllamaEmbedder backed by client using modelName.
func NewOllamaEmbedder(client *Client, modelName string) *OllamaEmbedder {
	return &OllamaEmbedder{client: client, modelName: modelName}
}

// Model returns the configured model name.
func (o *OllamaEmbedder) Model() string {
	return o.modelName
}

// Embed sends text to POST /api/embeddings using the configured model name.
// An empty embedding (zero-length slice) is treated as an error.
func (o *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	return o.client.embedWithModel(ctx, o.modelName, text)
}

// Embed sends text to POST /api/embeddings using the Client's configured model
// (OllamaEmbedder) or the prompt directly when called on a Client.
//
// The Client.Embed method is a convenience wrapper that uses the embedder model
// sent in the request. In OllamaEmbedder, the model is automatically filled in
// by the OllamaEmbedder.Embed call-through. When used standalone (e.g. in
// ProbeEmbed-alike tests), the model must be set separately.
//
// This method is NOT part of the Embedder interface; it is used internally by
// OllamaEmbedder.
func (c *Client) Embed(ctx context.Context, text string) ([]float32, error) {
	// OllamaEmbedder fills the model; for standalone use, this is a bare call.
	// We need a model name here. Since Client has no model field, embedder callers
	// must use OllamaEmbedder, which sets the model. For direct Client.Embed calls
	// in tests (without OllamaEmbedder), we pass an empty model — Ollama may default
	// or the test server doesn't care. This matches the ProbeEmbed pattern.
	return c.embedWithModel(ctx, "", text)
}

// embedWithModel is the shared implementation used by OllamaEmbedder and Client.Embed.
// model is the Ollama model name; when empty the server may use a default.
func (c *Client) embedWithModel(ctx context.Context, model, text string) ([]float32, error) {
	reqBody, err := json.Marshal(embedRequest{Model: model, Prompt: text})
	if err != nil {
		return nil, fmt.Errorf("embed: encoding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("/api/embeddings"),
		bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("embed: building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embed: unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("embed: reading response: %w", err)
	}

	var er embedResponse
	if err := json.Unmarshal(body, &er); err != nil {
		return nil, fmt.Errorf("embed: decoding response: %w", err)
	}

	if len(er.Embedding) == 0 {
		return nil, fmt.Errorf("embed: server returned empty embedding")
	}

	return er.Embedding, nil
}
