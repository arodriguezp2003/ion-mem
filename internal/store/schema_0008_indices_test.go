package store_test

import (
	"strings"
	"testing"

	"github.com/ionix/ion-mem/internal/store"
)

// TestMigration0008_CreatesHotPathIndices verifies that migration 0008 creates
// the two hot-path indices: idx_oe_model on observation_embeddings and the
// partial index idx_prompts_unconsumed on user_prompts.
func TestMigration0008_CreatesHotPathIndices(t *testing.T) {
	s := mustOpen(t)

	v := s.SchemaVersion()
	if v < 8 {
		t.Fatalf("expected SchemaVersion >= 8, got %d", v)
	}

	indexes := sqliteMasterNames(t, s, "index")
	for _, want := range []string{"idx_oe_model", "idx_prompts_unconsumed"} {
		if !contains(indexes, want) {
			t.Errorf("expected index %q in sqlite_master, got: %v", want, indexes)
		}
	}
}

// TestMigration0008_PartialIndexHasWhereClause verifies that the partial index
// for unconsumed prompts carries the expected WHERE predicate in its SQL
// definition. This proves the migration applied the right DDL, not just any
// index with the right name.
func TestMigration0008_PartialIndexHasWhereClause(t *testing.T) {
	s := mustOpen(t)

	var sql string
	err := s.DB().QueryRow(
		"SELECT sql FROM sqlite_master WHERE type='index' AND name='idx_prompts_unconsumed'",
	).Scan(&sql)
	if err != nil {
		t.Fatalf("querying idx_prompts_unconsumed sql: %v", err)
	}
	// The partial index must have a WHERE clause referencing consumed_at.
	if !strings.Contains(sql, "consumed_at") {
		t.Errorf("idx_prompts_unconsumed sql = %q, want to contain 'consumed_at'", sql)
	}
}

// TestMigration0008_Idempotent verifies that opening the same dataDir twice
// does not duplicate migration 0008 or fail.
func TestMigration0008_Idempotent(t *testing.T) {
	dir := t.TempDir()

	s1, err := store.Open(dir)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	v1 := s1.SchemaVersion()
	_ = s1.Close()

	s2, err := store.Open(dir)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer s2.Close()

	v2 := s2.SchemaVersion()
	if v2 != v1 {
		t.Fatalf("SchemaVersion mismatch after idempotent open: first=%d second=%d", v1, v2)
	}
}
