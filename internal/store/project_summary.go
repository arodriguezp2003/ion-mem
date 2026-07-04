package store

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// ProjectSummary holds per-project aggregate counts and last-activity timestamp
// for display in the TUI dashboard.
type ProjectSummary struct {
	Project          string
	ObservationCount int64
	SessionCount     int64
	LastActivity     time.Time // most recent observation updated_at for the project
}

// ProjectSummaries returns one summary per project that has at least one
// non-deleted observation, sorted alphabetically by project name.
// The LastActivity field reflects the most recent observation updated_at.
func (s *Store) ProjectSummaries(ctx context.Context) ([]ProjectSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			o.project,
			COUNT(o.id)          AS obs_count,
			MAX(o.updated_at)    AS last_activity
		FROM observations o
		WHERE o.deleted_at IS NULL
		GROUP BY o.project
	`)
	if err != nil {
		return nil, fmt.Errorf("store.ProjectSummaries: %w", err)
	}
	defer rows.Close()

	type rowResult struct {
		project      string
		obsCount     int64
		lastActivity string
	}

	var raw []rowResult
	for rows.Next() {
		var r rowResult
		if err := rows.Scan(&r.project, &r.obsCount, &r.lastActivity); err != nil {
			return nil, fmt.Errorf("store.ProjectSummaries scan: %w", err)
		}
		raw = append(raw, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store.ProjectSummaries rows: %w", err)
	}

	if len(raw) == 0 {
		return nil, nil
	}

	// Collect unique projects for a session count query.
	projects := make([]string, 0, len(raw))
	for _, r := range raw {
		projects = append(projects, r.project)
	}

	// Per-project session count.
	sessionCounts := make(map[string]int64, len(projects))
	for _, proj := range projects {
		var count int64
		if err := s.db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM sessions WHERE project=?", proj,
		).Scan(&count); err != nil {
			return nil, fmt.Errorf("store.ProjectSummaries session count for %q: %w", proj, err)
		}
		sessionCounts[proj] = count
	}

	summaries := make([]ProjectSummary, 0, len(raw))
	for _, r := range raw {
		t, err := parseISO(r.lastActivity)
		if err != nil {
			// Try RFC3339 fallback.
			t, err = time.Parse(time.RFC3339, r.lastActivity)
			if err != nil {
				t = time.Time{}
			}
		}
		summaries = append(summaries, ProjectSummary{
			Project:          r.project,
			ObservationCount: r.obsCount,
			SessionCount:     sessionCounts[r.project],
			LastActivity:     t,
		})
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Project < summaries[j].Project
	})

	return summaries, nil
}
