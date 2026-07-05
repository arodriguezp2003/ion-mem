package tui

// filter.go — Observations list: load-more pagination and type filter.
//
// Load-more:
//   When the cursor is on the LAST row and the last fetch returned exactly
//   obsPageSize rows (indicating there may be more), pressing j/↓ issues
//   fetchMoreObservations() which appends the next page to m.observations.
//   A LOADING… chip is shown in the status bar while the fetch is in flight.
//
// Type filter:
//   [F] cycles: ALL → each cycleableType → ALL (wraps).
//   Each cycle step refetches page 0 with the active type filter applied.
//   Status bar shows a FILTER: [BADGE] chip when a filter is active.
//   First Esc clears the filter (if active). Second Esc exits the view.

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ionix/ion-mem/internal/store"
)

// obsDefaultPageSize is the default page size for the observations list.
// Matches the previous hard-coded Limit of 50.
const obsDefaultPageSize = 50

// obsLoadMoreMsg is sent when the load-more fetch completes.
type obsLoadMoreMsg struct {
	observations []store.Observation
	// nextOffset is the offset to use for the NEXT load-more request.
	nextOffset int
	// rawCount is the number of rows returned by the DB before client-side
	// type filtering. Used to detect DB exhaustion (rawCount < obsPageSize).
	rawCount int
}

// ─── model fields (added to Model struct) ────────────────────────────────────
//
// obsPageSize  int    — page size for the observations list (default 50)
// obsOffset2   int    — SQL OFFSET for the next load-more request (page-based)
// obsLoading   bool   — true while a load-more fetch is in flight
// obsTypeFilter string — active type filter ("" = ALL)

// ─── commands ─────────────────────────────────────────────────────────────────

// fetchObservationsFiltered fetches the first page of observations with
// an optional type filter applied. Resets obsOffset2 to 0.
func (m Model) fetchObservationsFiltered() tea.Cmd {
	if m.store == nil {
		return nil
	}
	st := m.store
	project := m.selectedProject
	pageSize := m.obsPageSize
	if pageSize <= 0 {
		pageSize = obsDefaultPageSize
	}
	typeFilter := m.obsTypeFilter
	return func() tea.Msg {
		params := store.RecentObservationsParams{
			Project: project,
			Limit:   pageSize,
			Offset:  0,
		}
		_ = typeFilter // type filter applied TUI-side after fetch when no full-text search
		obs, err := st.RecentObservations(context.Background(), params)
		if err != nil {
			return errMsg{err}
		}
		rawCount := len(obs)
		// Apply type filter client-side (store.RecentObservations has no Type param).
		if typeFilter != "" {
			filtered := obs[:0]
			for _, o := range obs {
				if o.Type == typeFilter {
					filtered = append(filtered, o)
				}
			}
			obs = filtered
		}
		return observationsLoadedMsg{observations: obs, rawCount: rawCount, project: project}
	}
}

// fetchMoreObservations fetches the next page and appends to the existing list.
func (m Model) fetchMoreObservations() tea.Cmd {
	if m.store == nil {
		return nil
	}
	st := m.store
	project := m.selectedProject
	pageSize := m.obsPageSize
	if pageSize <= 0 {
		pageSize = obsDefaultPageSize
	}
	offset := m.obsOffset2
	typeFilter := m.obsTypeFilter
	return func() tea.Msg {
		params := store.RecentObservationsParams{
			Project: project,
			Limit:   pageSize,
			Offset:  offset,
		}
		obs, err := st.RecentObservations(context.Background(), params)
		if err != nil {
			return errMsg{err}
		}
		rawCount := len(obs)
		// Apply type filter client-side.
		if typeFilter != "" {
			filtered := obs[:0]
			for _, o := range obs {
				if o.Type == typeFilter {
					filtered = append(filtered, o)
				}
			}
			obs = filtered
		}
		return obsLoadMoreMsg{observations: obs, rawCount: rawCount, nextOffset: offset + pageSize}
	}
}

// ─── update handler helpers ──────────────────────────────────────────────────

// shouldLoadMore returns true when the cursor is at the last row and the DB
// may have more rows (i.e. not exhausted and not already loading).
func (m Model) shouldLoadMore() bool {
	if m.obsLoading {
		return false
	}
	if m.obsExhausted {
		return false
	}
	if m.obsPageSize <= 0 {
		return false
	}
	return m.obsCursor == len(m.observations)-1 && len(m.observations) > 0 &&
		len(m.observations)%m.obsPageSize == 0
}

// nextObsTypeFilter returns the next type in the cycle after current.
// Cycle: "" → types[0] → types[1] → … → types[n-1] → "" → …
func nextObsTypeFilter(current string) string {
	types := cycleableTypes()
	if len(types) == 0 {
		return ""
	}
	if current == "" {
		return types[0]
	}
	for i, t := range types {
		if t == current {
			next := i + 1
			if next >= len(types) {
				return "" // wrap back to ALL
			}
			return types[next]
		}
	}
	// current not in list → reset to ALL.
	return ""
}
