package store_test

import (
	"context"
	"math"
	"testing"

	"github.com/ionix/ion-mem/internal/store"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

// seedObsForEmbed inserts a minimal observation and returns its ID.
func seedObsForEmbed(t *testing.T, st *store.Store, title, project string) int64 {
	t.Helper()
	ctx := context.Background()
	// Ensure session exists.
	_, _ = st.CreateSession(ctx, store.CreateSessionParams{
		ID:        "embed-test-session",
		Project:   project,
		Directory: "/test",
	})
	obs, err := st.AddObservation(ctx, store.AddObservationParams{
		SessionID: "embed-test-session",
		Type:      "manual",
		Title:     title,
		Content:   "content for " + title,
		Project:   project,
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("seedObsForEmbed: %v", err)
	}
	return obs.ID
}

// ─── encode/decode round-trip ─────────────────────────────────────────────────

func TestEncodeDecodeVec_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		vec  []float32
	}{
		{"single value", []float32{1.5}},
		{"multiple values", []float32{0.1, -0.2, 0.3, 1000.0, -1000.0}},
		{"zeros", []float32{0, 0, 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blob := store.EncodeVec(tt.vec)
			got, err := store.DecodeVec(blob)
			if err != nil {
				t.Fatalf("DecodeVec: %v", err)
			}
			if len(got) != len(tt.vec) {
				t.Fatalf("len mismatch: got %d, want %d", len(got), len(tt.vec))
			}
			for i := range tt.vec {
				if got[i] != tt.vec[i] {
					t.Errorf("[%d]: got %v, want %v", i, got[i], tt.vec[i])
				}
			}
		})
	}
}

func TestDecodeVec_TruncatedDataReturnsError(t *testing.T) {
	blob := []byte{0x00, 0x01} // not a multiple of 4
	_, err := store.DecodeVec(blob)
	if err == nil {
		t.Error("DecodeVec truncated: expected error, got nil")
	}
}

// ─── cosine similarity ────────────────────────────────────────────────────────

func TestCosine_IdenticalVectors(t *testing.T) {
	a := []float32{1, 2, 3}
	got := store.Cosine(a, a)
	if math.Abs(got-1.0) > 1e-5 {
		t.Errorf("Cosine identical: got %.6f, want 1.0", got)
	}
}

func TestCosine_OrthogonalVectors(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{0, 1}
	got := store.Cosine(a, b)
	if math.Abs(got) > 1e-5 {
		t.Errorf("Cosine orthogonal: got %.6f, want 0.0", got)
	}
}

func TestCosine_ZeroVectorReturnsZero(t *testing.T) {
	a := []float32{0, 0, 0}
	b := []float32{1, 2, 3}
	got := store.Cosine(a, b)
	if got != 0.0 {
		t.Errorf("Cosine zero vector: got %v, want 0.0", got)
	}
}

func TestCosine_OppositeVectors(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{-1, 0}
	got := store.Cosine(a, b)
	if math.Abs(got-(-1.0)) > 1e-5 {
		t.Errorf("Cosine opposite: got %.6f, want -1.0", got)
	}
}

// ─── UpsertEmbedding / DeleteEmbedding ───────────────────────────────────────

func TestUpsertEmbedding_StoresAndOverwrites(t *testing.T) {
	ctx := context.Background()
	st := mustOpen(t)

	obsID := seedObsForEmbed(t, st, "embed-obs", "proj-e")
	vec1 := []float32{0.1, 0.2, 0.3}
	vec2 := []float32{0.4, 0.5, 0.6}

	// First insert.
	if err := st.UpsertEmbedding(ctx, obsID, "nomic-embed-text", vec1); err != nil {
		t.Fatalf("UpsertEmbedding first: %v", err)
	}

	// Overwrite.
	if err := st.UpsertEmbedding(ctx, obsID, "nomic-embed-text", vec2); err != nil {
		t.Fatalf("UpsertEmbedding overwrite: %v", err)
	}

	// Verify via VectorSearch that the latest vector is the one found.
	results, err := st.VectorSearch(ctx, vec2, store.SearchParams{Project: "proj-e", Limit: 5})
	if err != nil {
		t.Fatalf("VectorSearch after upsert: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("VectorSearch: expected at least one result, got none")
	}
	if results[0].Observation.ID != obsID {
		t.Errorf("VectorSearch top hit ID = %d, want %d", results[0].Observation.ID, obsID)
	}
}

func TestDeleteEmbedding_RemovesRow(t *testing.T) {
	ctx := context.Background()
	st := mustOpen(t)

	obsID := seedObsForEmbed(t, st, "del-embed-obs", "proj-d")
	vec := []float32{0.5, 0.5}
	if err := st.UpsertEmbedding(ctx, obsID, "nomic-embed-text", vec); err != nil {
		t.Fatalf("UpsertEmbedding: %v", err)
	}

	if err := st.DeleteEmbedding(ctx, obsID); err != nil {
		t.Fatalf("DeleteEmbedding: %v", err)
	}

	// After delete, VectorSearch should not return this observation.
	results, err := st.VectorSearch(ctx, vec, store.SearchParams{Project: "proj-d", Limit: 5})
	if err != nil {
		t.Fatalf("VectorSearch after delete: %v", err)
	}
	for _, r := range results {
		if r.Observation.ID == obsID {
			t.Errorf("DeleteEmbedding: obsID %d still appears in VectorSearch results", obsID)
		}
	}
}

// ─── EmbeddingCoverage / MissingEmbeddings ───────────────────────────────────

func TestEmbeddingCoverage_ReturnsHaveAndTotal(t *testing.T) {
	ctx := context.Background()
	st := mustOpen(t)
	proj := "cov-proj"
	model := "nomic-embed-text"

	// Seed 3 observations, embed 2.
	id1 := seedObsForEmbed(t, st, "cov-1", proj)
	id2 := seedObsForEmbed(t, st, "cov-2", proj)
	_ = seedObsForEmbed(t, st, "cov-3", proj)

	if err := st.UpsertEmbedding(ctx, id1, model, []float32{0.1, 0.2}); err != nil {
		t.Fatalf("UpsertEmbedding id1: %v", err)
	}
	if err := st.UpsertEmbedding(ctx, id2, model, []float32{0.3, 0.4}); err != nil {
		t.Fatalf("UpsertEmbedding id2: %v", err)
	}

	have, total, err := st.EmbeddingCoverage(ctx, proj, model)
	if err != nil {
		t.Fatalf("EmbeddingCoverage: %v", err)
	}
	if total != 3 {
		t.Errorf("EmbeddingCoverage total = %d, want 3", total)
	}
	if have != 2 {
		t.Errorf("EmbeddingCoverage have = %d, want 2", have)
	}
}

func TestMissingEmbeddings_ReturnsObsWithoutEmbedding(t *testing.T) {
	ctx := context.Background()
	st := mustOpen(t)
	proj := "miss-proj"
	model := "nomic-embed-text"

	id1 := seedObsForEmbed(t, st, "miss-1", proj)
	id2 := seedObsForEmbed(t, st, "miss-2", proj)
	id3 := seedObsForEmbed(t, st, "miss-3", proj)

	// Embed only id1.
	if err := st.UpsertEmbedding(ctx, id1, model, []float32{0.1}); err != nil {
		t.Fatalf("UpsertEmbedding: %v", err)
	}

	missing, err := st.MissingEmbeddings(ctx, proj, model, 100)
	if err != nil {
		t.Fatalf("MissingEmbeddings: %v", err)
	}

	// Only id2 and id3 should be missing.
	if len(missing) != 2 {
		t.Fatalf("MissingEmbeddings: got %d, want 2", len(missing))
	}
	missingIDs := make(map[int64]bool)
	for _, obs := range missing {
		missingIDs[obs.ID] = true
	}
	if missingIDs[id1] {
		t.Error("MissingEmbeddings: id1 (embedded) should not appear")
	}
	if !missingIDs[id2] || !missingIDs[id3] {
		t.Error("MissingEmbeddings: id2 and id3 (not embedded) should both appear")
	}
}

// ─── DeleteAllEmbeddings ─────────────────────────────────────────────────────

func TestDeleteAllEmbeddings_RemovesAllRows(t *testing.T) {
	ctx := context.Background()
	st := mustOpen(t)
	proj := "all-del-proj"
	model := "nomic-embed-text"

	id1 := seedObsForEmbed(t, st, "all-del-1", proj)
	id2 := seedObsForEmbed(t, st, "all-del-2", proj)

	if err := st.UpsertEmbedding(ctx, id1, model, []float32{0.1, 0.2}); err != nil {
		t.Fatalf("UpsertEmbedding id1: %v", err)
	}
	if err := st.UpsertEmbedding(ctx, id2, model, []float32{0.3, 0.4}); err != nil {
		t.Fatalf("UpsertEmbedding id2: %v", err)
	}

	n, err := st.DeleteAllEmbeddings(ctx)
	if err != nil {
		t.Fatalf("DeleteAllEmbeddings: %v", err)
	}
	if n != 2 {
		t.Errorf("DeleteAllEmbeddings rows affected = %d, want 2", n)
	}

	// After delete, VectorSearch should find nothing.
	results, err := st.VectorSearch(ctx, []float32{0.1, 0.2}, store.SearchParams{Project: proj, Limit: 5})
	if err != nil {
		t.Fatalf("VectorSearch after DeleteAllEmbeddings: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("VectorSearch after DeleteAllEmbeddings: got %d results, want 0", len(results))
	}
}

func TestDeleteAllEmbeddings_EmptyTableReturnsZero(t *testing.T) {
	ctx := context.Background()
	st := mustOpen(t)

	n, err := st.DeleteAllEmbeddings(ctx)
	if err != nil {
		t.Fatalf("DeleteAllEmbeddings on empty table: %v", err)
	}
	if n != 0 {
		t.Errorf("DeleteAllEmbeddings on empty table: got %d, want 0", n)
	}
}

func TestMissingEmbeddings_EmptyProjectReturnsAllProjects(t *testing.T) {
	ctx := context.Background()
	st := mustOpen(t)
	model := "nomic-embed-text"

	// Seed observations in two different projects.
	id1 := seedObsForEmbed(t, st, "ap-1", "proj-alpha")
	id2 := seedObsForEmbed(t, st, "ap-2", "proj-beta")
	// Embed only id1.
	if err := st.UpsertEmbedding(ctx, id1, model, []float32{0.1}); err != nil {
		t.Fatalf("UpsertEmbedding: %v", err)
	}

	// Empty project = all projects: id2 should appear as missing.
	missing, err := st.MissingEmbeddings(ctx, "", model, 100)
	if err != nil {
		t.Fatalf("MissingEmbeddings all-projects: %v", err)
	}
	found := false
	for _, obs := range missing {
		if obs.ID == id2 {
			found = true
		}
		if obs.ID == id1 {
			t.Error("MissingEmbeddings: id1 (embedded) should not appear")
		}
	}
	if !found {
		t.Error("MissingEmbeddings all-projects: id2 (not embedded) should appear")
	}
}

// ─── VectorSearch ─────────────────────────────────────────────────────────────

func TestVectorSearch_ReturnsClosestFirst(t *testing.T) {
	ctx := context.Background()
	st := mustOpen(t)
	proj := "vs-proj"
	model := "nomic-embed-text"

	id1 := seedObsForEmbed(t, st, "vs-near", proj)
	id2 := seedObsForEmbed(t, st, "vs-far", proj)

	// id1 is very close to the query vector; id2 is orthogonal.
	near := []float32{1.0, 0.0}
	far := []float32{0.0, 1.0}
	query := []float32{1.0, 0.01} // nearly identical to near

	if err := st.UpsertEmbedding(ctx, id1, model, near); err != nil {
		t.Fatalf("UpsertEmbedding near: %v", err)
	}
	if err := st.UpsertEmbedding(ctx, id2, model, far); err != nil {
		t.Fatalf("UpsertEmbedding far: %v", err)
	}

	results, err := st.VectorSearch(ctx, query, store.SearchParams{Project: proj, Limit: 5})
	if err != nil {
		t.Fatalf("VectorSearch: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("VectorSearch: got %d results, want >= 2", len(results))
	}
	if results[0].Observation.ID != id1 {
		t.Errorf("VectorSearch top hit = %d, want %d (near)", results[0].Observation.ID, id1)
	}
}

func TestVectorSearch_ExcludesSoftDeleted(t *testing.T) {
	ctx := context.Background()
	st := mustOpen(t)
	proj := "del-vs"
	model := "nomic-embed-text"

	obsID := seedObsForEmbed(t, st, "deleted-obs", proj)
	vec := []float32{1.0, 0.0}

	if err := st.UpsertEmbedding(ctx, obsID, model, vec); err != nil {
		t.Fatalf("UpsertEmbedding: %v", err)
	}
	if err := st.DeleteObservation(ctx, obsID, false); err != nil {
		t.Fatalf("DeleteObservation: %v", err)
	}

	results, err := st.VectorSearch(ctx, vec, store.SearchParams{Project: proj, Limit: 5})
	if err != nil {
		t.Fatalf("VectorSearch: %v", err)
	}
	for _, r := range results {
		if r.Observation.ID == obsID {
			t.Errorf("VectorSearch: soft-deleted obs %d should not appear in results", obsID)
		}
	}
}
