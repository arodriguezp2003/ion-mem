// Package embedjob provides a cancellable, observable embedding job runner
// shared by the CLI (backfill-embeddings) and the TUI (config view). It owns
// the fetch → embed → upsert loop, retry/backoff, abort/stop semantics, and
// progress reporting, so callers only need to consume a channel of Events.
package embedjob

import (
	"context"
	"time"

	"github.com/arodriguezp2003/ion-mem/internal/store"
)

// Kind selects which job the runner performs.
type Kind int

const (
	// KindEmbedMissing embeds every observation that does not yet have an
	// embedding row for the configured model. It never deletes existing rows.
	KindEmbedMissing Kind = iota
	// KindRegenerate deletes every existing embedding row first, then behaves
	// exactly like KindEmbedMissing (re-embeds everything from scratch).
	KindRegenerate
)

// String returns a short lowercase identifier used in log lines.
func (k Kind) String() string {
	switch k {
	case KindEmbedMissing:
		return "embed-missing"
	case KindRegenerate:
		return "regenerate"
	default:
		return "unknown"
	}
}

// Status describes how a job run ended.
type Status int

const (
	// StatusComplete means every observation that needed an embedding got one.
	StatusComplete Status = iota
	// StatusPartial means the job finished its pass but at least one item
	// exhausted its retries and was skipped.
	StatusPartial
	// StatusAborted means the job stopped itself early: either the warm-up
	// probe failed, or too many consecutive item failures were observed.
	StatusAborted
	// StatusStopped means the caller's context was cancelled mid-run.
	StatusStopped
)

// String returns a short lowercase identifier used in log lines and CLI output.
func (s Status) String() string {
	switch s {
	case StatusComplete:
		return "complete"
	case StatusPartial:
		return "partial"
	case StatusAborted:
		return "aborted"
	case StatusStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

// EventType identifies the kind of progress Event emitted by Run.
type EventType int

const (
	// EventStarted is emitted once, immediately, before any work happens.
	EventStarted EventType = iota
	// EventWarmedUp is emitted once after a successful warm-up probe (only
	// when Config.WarmUp is true).
	EventWarmedUp
	// EventItem is emitted once per observation processed, success or failure.
	EventItem
	// EventDone is emitted exactly once, as the final event before the
	// channel returned by Run is closed.
	EventDone
)

// Store is the subset of *store.Store that embedjob needs. It exists so
// tests can supply an in-memory fake instead of a real SQLite-backed store.
type Store interface {
	MissingEmbeddings(ctx context.Context, project, model string, limit int) ([]store.Observation, error)
	UpsertEmbedding(ctx context.Context, obsID int64, model string, vec []float32) error
	EmbeddingCoverage(ctx context.Context, project, model string) (have, total int, err error)
	DeleteAllEmbeddings(ctx context.Context) (int64, error)
}

// Config controls a single Run. The zero value of most fields is treated as
// "unset" by withDefaults, which fills in a sane production default.
//
// WarmUp and Retries are the two exceptions, because for both of them the
// Go zero value is itself a meaningful, legitimate setting that must not be
// silently overridden:
//
//   - WarmUp: Go's bool zero value (false) cannot be distinguished from an
//     explicit "disable warm-up", so WarmUp is NOT defaulted to true by
//     withDefaults. Callers that want the warm-up probe (the CLI and TUI
//     both do) must set WarmUp: true explicitly.
//   - Retries: 0 means "a single attempt, no retries" — a legitimate and
//     sometimes deliberate choice (e.g. MaxConsecutiveFailures-driven abort
//     tests) — so withDefaults leaves it alone. Callers that want the
//     historical default of DefaultRetries retries must set
//     Retries: DefaultRetries explicitly.
type Config struct {
	Kind    Kind
	Project string // "" = all projects
	Batch   int    // page size, default 25

	ItemTimeout time.Duration   // per embed call, default 30s
	Retries     int             // per item; 0 = no retries (single attempt); see doc comment above
	Backoff     []time.Duration // default {1s, 3s}; index = attempt-1, last value repeats

	MaxConsecutiveFailures int // default 10 -> abort

	WarmUp bool // see doc comment above: no zero-value default

	// Sleep is called between retry attempts. It is injectable so tests can
	// run the retry/backoff paths without real wall-clock delays. The
	// default is a context-aware sleep that returns ctx.Err() early if ctx
	// is cancelled while waiting.
	Sleep func(ctx context.Context, d time.Duration) error
}

const (
	defaultBatch                  = 25
	defaultItemTimeout            = 30 * time.Second
	defaultMaxConsecutiveFailures = 10
	warmUpTimeout                 = 90 * time.Second
	warmUpText                    = "warm-up"
	eventBufferSize               = 64
)

// DefaultRetries is the historical number of retries per item (2, i.e. up to
// 3 attempts). It is exported so callers that want that behavior can request
// it explicitly — Config's zero value for Retries means "no retries" (see
// the Config doc comment), so this is NOT applied automatically.
const DefaultRetries = 2

// defaultBackoff is used when Config.Backoff is empty.
func defaultBackoff() []time.Duration {
	return []time.Duration{1 * time.Second, 3 * time.Second}
}

// withDefaults returns a copy of cfg with every zero-valued field (other than
// WarmUp and Retries, see the Config doc comment) replaced by its production
// default.
func (c Config) withDefaults() Config {
	if c.Batch <= 0 {
		c.Batch = defaultBatch
	}
	if c.ItemTimeout <= 0 {
		c.ItemTimeout = defaultItemTimeout
	}
	if c.Retries < 0 {
		// Negative retries is not a meaningful setting (unlike 0, which
		// legitimately means "no retries"); clamp it so attemptEmbed's
		// 1+Retries attempt count never degenerates to zero attempts.
		c.Retries = 0
	}
	if len(c.Backoff) == 0 {
		c.Backoff = defaultBackoff()
	}
	if c.MaxConsecutiveFailures <= 0 {
		c.MaxConsecutiveFailures = defaultMaxConsecutiveFailures
	}
	if c.Sleep == nil {
		c.Sleep = defaultSleep
	}
	return c
}

// defaultSleep waits for d, or returns ctx.Err() early if ctx is cancelled
// first. d <= 0 returns immediately.
func defaultSleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// backoffFor returns the backoff duration for the attempt that just failed
// (1-based attempt number, so idx = attempt-1). The last configured value
// repeats for every attempt beyond len(backoff).
func backoffFor(backoff []time.Duration, idx int) time.Duration {
	if len(backoff) == 0 {
		return 0
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(backoff) {
		idx = len(backoff) - 1
	}
	return backoff[idx]
}

// Event is one progress notification emitted while a job runs. Done, Failed
// and Total are running counters present on every event; Summary is set only
// on the final EventDone.
type Event struct {
	Type EventType

	ID       int64         // EventItem
	Title    string        // EventItem
	Duration time.Duration // EventItem (success) / EventWarmedUp
	Attempt  int           // EventItem: attempts used
	Err      error         // EventItem failure

	Done, Failed, Total int // running counters, present on every event

	Summary *Summary // EventDone only
}

// Summary is the final report attached to the terminal EventDone.
type Summary struct {
	Status              Status
	Done, Failed, Total int
	Elapsed             time.Duration
	LastErr             error
	LogPath             string
}
