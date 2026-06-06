package store

import (
	"context"
	"fmt"
	"sort"
)

// TimelineEntry represents one item in a session timeline. Kind is either
// "observation" or "prompt". Exactly one of Observation or Prompt is non-nil.
type TimelineEntry struct {
	Kind        string
	Observation *Observation
	Prompt      *Prompt
	// createdAt is the RFC3339Nano string used for internal sorting only.
	createdAt string
}

// Stats holds aggregate counts across the entire store.
type Stats struct {
	TotalSessions     int64
	TotalObservations int64 // non-deleted only
	TotalPrompts      int64
	ByProject         []ProjectStats
}

// ProjectStats holds per-project aggregate counts.
type ProjectStats struct {
	Project          string
	ObservationCount int64 // non-deleted only
	PromptCount      int64
}

// Timeline returns observations and prompts from the same session as
// observationID, ordered chronologically. It returns up to `before` entries
// created before the anchor and up to `after` entries created after.
//
// Returns ErrObservationNotFound when observationID does not exist or is
// soft-deleted.
func (s *Store) Timeline(ctx context.Context, observationID int64, before, after int) ([]TimelineEntry, error) {
	// Fetch the anchor observation to get session_id and created_at.
	anchor, err := s.getObservationByID(ctx, observationID)
	if err != nil {
		// getObservationByID returns ErrObservationNotFound for missing/soft-deleted.
		return nil, err
	}

	sessionID := anchor.SessionID
	anchorCreatedAt := anchor.CreatedAt

	// Collect all non-deleted observations in the same session.
	obsRows, err := s.db.QueryContext(ctx, `
		SELECT id, sync_id, session_id, type, title, content, tool_name,
		       project, scope, topic_key, normalized_hash,
		       revision_count, duplicate_count, last_seen_at,
		       created_at, updated_at, deleted_at
		FROM observations
		WHERE session_id=? AND deleted_at IS NULL
		ORDER BY created_at ASC`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("store.Timeline observations: %w", err)
	}
	defer obsRows.Close()

	var allEntries []TimelineEntry
	for obsRows.Next() {
		o, err := scanObservationRow(obsRows)
		if err != nil {
			return nil, fmt.Errorf("store.Timeline obs scan: %w", err)
		}
		oCopy := o
		allEntries = append(allEntries, TimelineEntry{
			Kind:        "observation",
			Observation: &oCopy,
			createdAt:   o.CreatedAt,
		})
	}
	if err := obsRows.Err(); err != nil {
		return nil, fmt.Errorf("store.Timeline obs rows: %w", err)
	}

	// Collect all prompts in the same session.
	promptRows, err := s.db.QueryContext(ctx, `
		SELECT id, sync_id, session_id, content, project, created_at
		FROM user_prompts
		WHERE session_id=?
		ORDER BY created_at ASC`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("store.Timeline prompts: %w", err)
	}
	defer promptRows.Close()

	for promptRows.Next() {
		p, err := scanPromptRow(promptRows)
		if err != nil {
			return nil, fmt.Errorf("store.Timeline prompt scan: %w", err)
		}
		pCopy := p
		allEntries = append(allEntries, TimelineEntry{
			Kind:      "prompt",
			Prompt:    &pCopy,
			createdAt: p.CreatedAt,
		})
	}
	if err := promptRows.Err(); err != nil {
		return nil, fmt.Errorf("store.Timeline prompt rows: %w", err)
	}

	// Sort all entries chronologically by created_at.
	sort.Slice(allEntries, func(i, j int) bool {
		return allEntries[i].createdAt < allEntries[j].createdAt
	})

	// Find the anchor index.
	anchorIdx := -1
	for i, e := range allEntries {
		if e.Kind == "observation" && e.Observation.ID == observationID {
			anchorIdx = i
			break
		}
	}
	if anchorIdx < 0 {
		// Anchor should always be found since we fetched it first; guard anyway.
		_ = anchorCreatedAt
		return nil, ErrObservationNotFound
	}

	// Compute window bounds.
	start := anchorIdx - before
	if start < 0 {
		start = 0
	}
	end := anchorIdx + after + 1
	if end > len(allEntries) {
		end = len(allEntries)
	}

	return allEntries[start:end], nil
}

// Stats returns aggregate counts for the entire store.
// All queries run in a single transaction for consistency.
func (s *Store) Stats(ctx context.Context) (Stats, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Stats{}, fmt.Errorf("store.Stats begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var st Stats

	// Total sessions.
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM sessions").Scan(&st.TotalSessions); err != nil {
		return Stats{}, fmt.Errorf("store.Stats total sessions: %w", err)
	}

	// Total non-deleted observations.
	if err := tx.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM observations WHERE deleted_at IS NULL",
	).Scan(&st.TotalObservations); err != nil {
		return Stats{}, fmt.Errorf("store.Stats total observations: %w", err)
	}

	// Total prompts.
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM user_prompts").Scan(&st.TotalPrompts); err != nil {
		return Stats{}, fmt.Errorf("store.Stats total prompts: %w", err)
	}

	// Per-project observation counts (non-deleted).
	obsRows, err := tx.QueryContext(ctx, `
		SELECT project, COUNT(*) FROM observations
		WHERE deleted_at IS NULL
		GROUP BY project`)
	if err != nil {
		return Stats{}, fmt.Errorf("store.Stats per-project obs: %w", err)
	}
	defer obsRows.Close()

	projectMap := make(map[string]*ProjectStats)
	for obsRows.Next() {
		var project string
		var count int64
		if err := obsRows.Scan(&project, &count); err != nil {
			return Stats{}, fmt.Errorf("store.Stats obs scan: %w", err)
		}
		if _, ok := projectMap[project]; !ok {
			projectMap[project] = &ProjectStats{Project: project}
		}
		projectMap[project].ObservationCount = count
	}
	if err := obsRows.Err(); err != nil {
		return Stats{}, fmt.Errorf("store.Stats obs rows: %w", err)
	}

	// Per-project prompt counts.
	pRows, err := tx.QueryContext(ctx, `
		SELECT project, COUNT(*) FROM user_prompts GROUP BY project`)
	if err != nil {
		return Stats{}, fmt.Errorf("store.Stats per-project prompts: %w", err)
	}
	defer pRows.Close()

	for pRows.Next() {
		var project string
		var count int64
		if err := pRows.Scan(&project, &count); err != nil {
			return Stats{}, fmt.Errorf("store.Stats prompt scan: %w", err)
		}
		if _, ok := projectMap[project]; !ok {
			projectMap[project] = &ProjectStats{Project: project}
		}
		projectMap[project].PromptCount = count
	}
	if err := pRows.Err(); err != nil {
		return Stats{}, fmt.Errorf("store.Stats prompt rows: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Stats{}, fmt.Errorf("store.Stats commit: %w", err)
	}

	// Flatten projectMap into slice.
	st.ByProject = make([]ProjectStats, 0, len(projectMap))
	for _, ps := range projectMap {
		st.ByProject = append(st.ByProject, *ps)
	}
	sort.Slice(st.ByProject, func(i, j int) bool {
		return st.ByProject[i].Project < st.ByProject[j].Project
	})

	return st, nil
}
