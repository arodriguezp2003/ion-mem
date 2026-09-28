package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/arodriguezp2003/ion-mem/internal/eval"
	"github.com/arodriguezp2003/ion-mem/internal/store"
)

// fakeBackfillEmbedder is a minimal embed.Embedder for backfillEmbeddings
// tests: it always succeeds with a fixed vector (embed failures are already
// covered end-to-end via the httptest fakeOllamaServer in the other eval CLI
// tests; this file isolates the upsert-failure handling specifically).
type fakeBackfillEmbedder struct{}

func (fakeBackfillEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return []float32{0.1, 0.2}, nil
}
func (fakeBackfillEmbedder) Model() string { return "fake-model" }

func TestBackfillEmbeddingsWithUpsert_NoMissingDocs_NoError(t *testing.T) {
	var stderr strings.Builder
	err := backfillEmbeddingsWithUpsert(context.Background(), nil, fakeBackfillEmbedder{}, evalConfig{model: "m", mode: eval.ModeHybrid}, &stderr,
		func(context.Context, int64, string, []float32) error {
			t.Fatal("upsert should not be called when there are no missing docs")
			return nil
		})
	if err != nil {
		t.Fatalf("backfillEmbeddingsWithUpsert: %v", err)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr should be empty, got %q", stderr.String())
	}
}

func TestBackfillEmbeddingsWithUpsert_PartialFailure_LogsAndContinues(t *testing.T) {
	missing := []store.Observation{
		{ID: 1, Title: "doc one", Content: "content one"},
		{ID: 2, Title: "doc two", Content: "content two"},
	}
	upsertErr := errors.New("disk full")

	var stderr strings.Builder
	var upserted []int64
	err := backfillEmbeddingsWithUpsert(context.Background(), missing, fakeBackfillEmbedder{}, evalConfig{model: "m", mode: eval.ModeHybrid}, &stderr,
		func(_ context.Context, obsID int64, _ string, _ []float32) error {
			upserted = append(upserted, obsID)
			if obsID == 1 {
				return upsertErr
			}
			return nil
		})
	if err != nil {
		t.Fatalf("backfillEmbeddingsWithUpsert: partial failure should not abort the run: %v", err)
	}
	if len(upserted) != 2 {
		t.Fatalf("want both upserts attempted, got %v", upserted)
	}
	logged := stderr.String()
	if !strings.Contains(logged, "doc one") {
		t.Errorf("stderr should mention the failing doc's title %q; got %q", "doc one", logged)
	}
	if !strings.Contains(logged, "disk full") {
		t.Errorf("stderr should mention the underlying error; got %q", logged)
	}
	if strings.Contains(logged, "doc two") {
		t.Errorf("stderr should NOT mention the successfully-upserted doc; got %q", logged)
	}
}

func TestBackfillEmbeddingsWithUpsert_AllFail_ReturnsError(t *testing.T) {
	missing := []store.Observation{
		{ID: 1, Title: "doc one", Content: "x"},
		{ID: 2, Title: "doc two", Content: "y"},
		{ID: 3, Title: "doc three", Content: "z"},
	}
	upsertErr := errors.New("constraint violation")

	var stderr strings.Builder
	err := backfillEmbeddingsWithUpsert(context.Background(), missing, fakeBackfillEmbedder{}, evalConfig{model: "m", mode: eval.ModeHybrid}, &stderr,
		func(context.Context, int64, string, []float32) error {
			return upsertErr
		})
	if err == nil {
		t.Fatal("expected an error when every upsert fails")
	}
	if !strings.Contains(err.Error(), "3") {
		t.Errorf("error %q should mention the failure count (3)", err.Error())
	}
	if !strings.Contains(stderr.String(), "doc one") || !strings.Contains(stderr.String(), "doc three") {
		t.Errorf("stderr should still log every failure before returning the error; got %q", stderr.String())
	}
}

// fakeEmbedderWithFailures fails to embed any text containing "unembeddable",
// succeeding otherwise — used to verify a doc that fails to EMBED (as
// opposed to failing to UPSERT) is correctly excluded from the "all upserts
// failed" count, since it never reaches the upsert step at all.
type fakeEmbedderWithFailures struct{}

func (fakeEmbedderWithFailures) Embed(_ context.Context, text string) ([]float32, error) {
	if strings.Contains(text, "unembeddable") {
		return nil, errors.New("embed failed")
	}
	return []float32{0.1, 0.2}, nil
}
func (fakeEmbedderWithFailures) Model() string { return "fake-model" }

func TestBackfillEmbeddingsWithUpsert_EmbedFailureExcludedFromUpsertCount(t *testing.T) {
	missing := []store.Observation{
		{ID: 1, Title: "unembeddable doc", Content: "x"}, // fails to embed; never reaches upsert
		{ID: 2, Title: "good doc", Content: "y"},         // embeds fine, upsert succeeds
	}

	var stderr strings.Builder
	var upsertCalls int
	err := backfillEmbeddingsWithUpsert(context.Background(), missing, fakeEmbedderWithFailures{}, evalConfig{model: "m", mode: eval.ModeHybrid}, &stderr,
		func(context.Context, int64, string, []float32) error {
			upsertCalls++
			return nil
		})
	if err != nil {
		t.Fatalf("backfillEmbeddingsWithUpsert: %v", err)
	}
	if upsertCalls != 1 {
		t.Errorf("upsert should only be attempted for the successfully-embedded doc, got %d calls", upsertCalls)
	}
	if stderr.Len() != 0 {
		t.Errorf("no upsert failures occurred; stderr should be empty, got %q", stderr.String())
	}
}
