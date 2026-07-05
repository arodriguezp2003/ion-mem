package store

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
)

// ─── encode / decode ─────────────────────────────────────────────────────────

// EncodeVec serialises a float32 slice as a little-endian binary BLOB.
// Each float32 occupies exactly 4 bytes.
func EncodeVec(vec []float32) []byte {
	buf := make([]byte, len(vec)*4)
	for i, v := range vec {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

// DecodeVec deserialises a little-endian binary BLOB into a float32 slice.
// Returns an error when len(blob) is not a multiple of 4.
func DecodeVec(blob []byte) ([]float32, error) {
	if len(blob)%4 != 0 {
		return nil, fmt.Errorf("store.DecodeVec: blob length %d is not a multiple of 4", len(blob))
	}
	out := make([]float32, len(blob)/4)
	for i := range out {
		bits := binary.LittleEndian.Uint32(blob[i*4:])
		out[i] = math.Float32frombits(bits)
	}
	return out, nil
}

// ─── cosine similarity ────────────────────────────────────────────────────────

// Cosine computes the cosine similarity between two float32 vectors.
// Returns 0 when either vector is a zero vector (guard against division by zero).
// The result is in [-1, 1]: 1 = identical direction, 0 = orthogonal, -1 = opposite.
func Cosine(a, b []float32) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	var dot, normA, normB float64
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		fa, fb := float64(a[i]), float64(b[i])
		dot += fa * fb
		normA += fa * fa
		normB += fb * fb
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// ─── Store methods ────────────────────────────────────────────────────────────

// UpsertEmbedding inserts or replaces the embedding row for obsID.
// vec is serialised as a little-endian float32 BLOB.
func (s *Store) UpsertEmbedding(ctx context.Context, obsID int64, model string, vec []float32) error {
	blob := EncodeVec(vec)
	now := nowISO()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO observation_embeddings (observation_id, model, dims, vector, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(observation_id) DO UPDATE
		    SET model = excluded.model,
		        dims  = excluded.dims,
		        vector = excluded.vector,
		        updated_at = excluded.updated_at
	`, obsID, model, len(vec), blob, now)
	if err != nil {
		return fmt.Errorf("store.UpsertEmbedding: %w", err)
	}
	return nil
}

// DeleteAllEmbeddings removes every row from observation_embeddings and returns
// the number of rows deleted. This is used by the REGENERATE EMBEDDINGS action
// in the config view to force a full re-index.
func (s *Store) DeleteAllEmbeddings(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, "DELETE FROM observation_embeddings")
	if err != nil {
		return 0, fmt.Errorf("store.DeleteAllEmbeddings: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store.DeleteAllEmbeddings rows affected: %w", err)
	}
	return n, nil
}

// DeleteEmbedding removes the embedding row for obsID (best-effort; no error
// when the row does not exist).
func (s *Store) DeleteEmbedding(ctx context.Context, obsID int64) error {
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM observation_embeddings WHERE observation_id = ?", obsID)
	if err != nil {
		return fmt.Errorf("store.DeleteEmbedding: %w", err)
	}
	return nil
}

// EmbeddingCoverage returns the number of non-deleted observations in project
// that have an embedding row for model (have) and the total count of non-deleted
// observations in project (total). When project is empty the query spans all
// projects.
func (s *Store) EmbeddingCoverage(ctx context.Context, project, model string) (have, total int, err error) {
	if project == "" {
		err = s.db.QueryRowContext(ctx, `
			SELECT
			    COUNT(oe.observation_id),
			    COUNT(o.id)
			FROM observations o
			LEFT JOIN observation_embeddings oe
			    ON oe.observation_id = o.id AND oe.model = ?
			WHERE o.deleted_at IS NULL
		`, model).Scan(&have, &total)
	} else {
		err = s.db.QueryRowContext(ctx, `
			SELECT
			    COUNT(oe.observation_id),
			    COUNT(o.id)
			FROM observations o
			LEFT JOIN observation_embeddings oe
			    ON oe.observation_id = o.id AND oe.model = ?
			WHERE o.project = ?
			  AND o.deleted_at IS NULL
		`, model, project).Scan(&have, &total)
	}
	if err != nil {
		return 0, 0, fmt.Errorf("store.EmbeddingCoverage: %w", err)
	}
	return have, total, nil
}

// MissingEmbeddings returns up to limit non-deleted observations in project
// that do not yet have an embedding row for model. When project is empty the
// query spans all projects.
func (s *Store) MissingEmbeddings(ctx context.Context, project, model string, limit int) ([]Observation, error) {
	if limit <= 0 {
		limit = 50
	}
	var (
		rows *sql.Rows
		err  error
	)
	if project == "" {
		rows, err = s.db.QueryContext(ctx, `
			SELECT o.id, o.sync_id, o.session_id, o.type, o.title, o.content, o.tool_name,
			       o.project, o.scope, o.topic_key, o.normalized_hash,
			       o.revision_count, o.duplicate_count, o.last_seen_at,
			       o.created_at, o.updated_at, o.deleted_at
			FROM observations o
			LEFT JOIN observation_embeddings oe
			    ON oe.observation_id = o.id AND oe.model = ?
			WHERE o.deleted_at IS NULL
			  AND oe.observation_id IS NULL
			ORDER BY o.id ASC
			LIMIT ?
		`, model, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT o.id, o.sync_id, o.session_id, o.type, o.title, o.content, o.tool_name,
			       o.project, o.scope, o.topic_key, o.normalized_hash,
			       o.revision_count, o.duplicate_count, o.last_seen_at,
			       o.created_at, o.updated_at, o.deleted_at
			FROM observations o
			LEFT JOIN observation_embeddings oe
			    ON oe.observation_id = o.id AND oe.model = ?
			WHERE o.project = ?
			  AND o.deleted_at IS NULL
			  AND oe.observation_id IS NULL
			ORDER BY o.id ASC
			LIMIT ?
		`, model, project, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("store.MissingEmbeddings: %w", err)
	}
	defer rows.Close()

	var out []Observation
	for rows.Next() {
		obs, err := scanObservationRow(rows)
		if err != nil {
			return nil, fmt.Errorf("store.MissingEmbeddings scan: %w", err)
		}
		out = append(out, obs)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store.MissingEmbeddings rows: %w", err)
	}
	return out, nil
}

// VectorSearch performs a brute-force cosine similarity search over all
// non-deleted observations in the filter scope that have an embedding row.
//
// Score convention: Score = -similarity so that "lower is better" is consistent
// with the BM25 convention used by SearchResult (negative = more relevant).
// Score -1.0 means perfect match; Score +1.0 means exact opposite.
//
// The design choice of brute force (no ANN index) is intentional: modernc/sqlite
// is pure-Go with no CGO, making external ANN libraries impractical. At the
// expected scale of tens of thousands of observations the in-memory loop is fast.
func (s *Store) VectorSearch(ctx context.Context, queryVec []float32, params SearchParams) ([]SearchResult, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 20
	}

	// Load candidate vectors for the filter scope.
	query := `
		SELECT o.id, o.sync_id, o.session_id, o.type, o.title, o.content, o.tool_name,
		       o.project, o.scope, o.topic_key, o.normalized_hash,
		       o.revision_count, o.duplicate_count, o.last_seen_at,
		       o.created_at, o.updated_at, o.deleted_at,
		       oe.vector
		FROM observation_embeddings oe
		JOIN observations o ON o.id = oe.observation_id
		WHERE o.deleted_at IS NULL`
	args := []interface{}{}

	if params.Project != "" {
		query += " AND o.project = ?"
		args = append(args, params.Project)
	}
	if params.Type != "" {
		query += " AND o.type = ?"
		args = append(args, params.Type)
	}
	if params.Scope != "" {
		query += " AND o.scope = ?"
		args = append(args, params.Scope)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store.VectorSearch: %w", err)
	}
	defer rows.Close()

	type candidate struct {
		obs  Observation
		blob []byte
	}
	var candidates []candidate

	for rows.Next() {
		var obs Observation
		var toolName, topicKey, deletedAt sql.NullString
		var blob []byte
		if err := rows.Scan(
			&obs.ID, &obs.SyncID, &obs.SessionID, &obs.Type, &obs.Title, &obs.Content, &toolName,
			&obs.Project, &obs.Scope, &topicKey, &obs.NormalizedHash,
			&obs.RevisionCount, &obs.DuplicateCount, &obs.LastSeenAt,
			&obs.CreatedAt, &obs.UpdatedAt, &deletedAt,
			&blob,
		); err != nil {
			return nil, fmt.Errorf("store.VectorSearch scan: %w", err)
		}
		if toolName.Valid {
			obs.ToolName = &toolName.String
		}
		if topicKey.Valid {
			obs.TopicKey = &topicKey.String
		}
		if deletedAt.Valid {
			obs.DeletedAt = &deletedAt.String
		}
		candidates = append(candidates, candidate{obs: obs, blob: blob})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store.VectorSearch rows: %w", err)
	}

	// Compute cosine similarity for each candidate.
	results := make([]SearchResult, 0, len(candidates))
	for _, c := range candidates {
		vec, err := DecodeVec(c.blob)
		if err != nil {
			// Skip malformed rows rather than aborting the entire search.
			continue
		}
		sim := Cosine(queryVec, vec)
		// Score = -similarity: lower is better, consistent with BM25 convention.
		results = append(results, SearchResult{
			Observation: c.obs,
			Score:       -sim,
		})
	}

	// Sort ascending by Score (most negative = highest similarity = best match).
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score < results[j].Score
	})

	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}
