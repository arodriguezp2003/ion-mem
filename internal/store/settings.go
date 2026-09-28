package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Setting key constants for embeddings configuration.
const (
	SettingEmbeddingsEnabled = "embeddings.enabled"
	SettingOllamaURL         = "embeddings.ollama_url"
	SettingEmbeddingsModel   = "embeddings.model"

	// SettingLogVerbose controls whether joblog-backed jobs (e.g.
	// backfill-embeddings) emit DEBUG-level lines in addition to INFO.
	SettingLogVerbose = "log.verbose"

	// SettingSearchMode selects the retrieval strategy used by
	// internal/hybrid.Searcher: "lexical", "vector", or "hybrid" (the values
	// match internal/hybrid.Mode's string constants exactly). See
	// DefaultSearchMode for the rationale behind the default.
	SettingSearchMode = "search.mode"

	// SettingOllamaAutostart gates whether `ion-mem doctor --autostart` (and
	// the SessionStart hook, which invokes it) is allowed to launch a local
	// Ollama server when it is configured but unreachable. Off by default:
	// autostarting a background process is a meaningful side effect that
	// operators should opt into explicitly.
	SettingOllamaAutostart = "ollama.autostart"
)

// DefaultEmbeddingsModel is the embedding model used when embeddings.model is
// not set. bge-m3 (1024 dims) is multilingual and handles cross-lingual
// retrieval (e.g. Spanish query against English content); nomic-embed-text does
// not, so it must never be the default.
const DefaultEmbeddingsModel = "bge-m3"

// DefaultSearchMode is the search.mode value used when unset.
//
// Benchmarked on the real corpus (2026-09-26): BM25-alone MRR 0.42;
// production hybrid (fuse everything, vector weight 2.0) 0.75; vector-only
// (bge-m3) 0.90; hybrid-strict (fuse BM25 only when the AND query matched,
// skip the noisy OR fuzzy fallback) 0.91. 38/42 natural-language queries fell
// into the OR fallback, which is both noisy and ~5x slower than a single
// vector search. Vector-only gets 99% of hybrid-strict's quality at a
// fraction of the latency and complexity, so it is the default; hybrid
// remains available (in its strict form) for callers that also want lexical
// recall on exact-term queries.
const DefaultSearchMode = "vector"

// DefaultOllamaAutostart is the ollama.autostart value used when unset.
const DefaultOllamaAutostart = "false"

// GetSetting retrieves the value stored for key. Returns (value, true, nil) when
// found, ("", false, nil) when not found, or ("", false, err) on a database error.
func (s *Store) GetSetting(ctx context.Context, key string) (string, bool, error) {
	var val string
	err := s.db.QueryRowContext(ctx,
		"SELECT value FROM settings WHERE key = ?", key,
	).Scan(&val)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return val, true, nil
}

// SetSetting upserts the value for key. The updated_at timestamp is recorded
// as a UTC RFC3339 string. Calling SetSetting again with the same key
// overwrites the previous value (upsert semantics).
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	updatedAt := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO settings (key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
	`, key, value, updatedAt)
	return err
}

// SettingOrDefault returns the stored value for key, or def if the key is not
// found or a database error occurs. Callers that need to distinguish "not set"
// from an error should use GetSetting directly.
func (s *Store) SettingOrDefault(ctx context.Context, key, def string) string {
	val, ok, err := s.GetSetting(ctx, key)
	if err != nil || !ok {
		return def
	}
	return val
}
