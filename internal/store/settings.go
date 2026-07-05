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
)

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
