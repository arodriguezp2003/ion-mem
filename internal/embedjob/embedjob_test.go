package embedjob_test

// embedjob_test.go exercises the Run() job engine against a fake Store and a
// fake embed.Embedder, so the retry/backoff/abort/stop semantics can be tested
// without any real database or HTTP server.
//
// TDD cycle for this file:
//  1. TestRun_CompleteRun                              — happy path, all items embedded.
//  2. TestRun_RetryThenSuccess                          — one item fails once, then succeeds.
//  3. TestRun_ItemExhaustsRetries_PartialNotRefetched    — one item always fails, Partial result.
//  4. TestRun_ConsecutiveFailuresAborted                — N consecutive failures aborts the job.
//  5. TestRun_CtxCancelMidRun_Stopped                    — cancelling ctx mid-run yields Stopped.
//  6. TestRun_Regenerate_DeletesAllEmbeddingsFirst        — KindRegenerate wipes embeddings first.
//  7. TestRun_WarmUpFailure_Aborted                       — warm-up failure aborts before any item.
//  8. TestRun_EveryRunEndsWithExactlyOneEventDone         — invariant check across scenarios.
//  9. TestRun_CountersAreMonotonic                        — Done/Failed never decrease across events.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arodriguezp2003/ion-mem/internal/embedjob"
	"github.com/arodriguezp2003/ion-mem/internal/store"
)

// ─── fake Store ───────────────────────────────────────────────────────────────

// fakeObs is a minimal in-memory stand-in for a stored observation.
type fakeObs struct {
	id      int64
	title   string
	content string
	project string
}

// fakeStore implements embedjob.Store entirely in memory, mirroring the
// semantics of *store.Store: MissingEmbeddings returns rows ordered by id
// ascending that lack an embedding row for model, so a row that keeps failing
// is re-fetched on every page unless the caller filters it out itself (which
// is exactly why Run() tracks a "failed" set of ids to skip).
type fakeStore struct {
	mu            sync.Mutex
	obs           []fakeObs
	embeddings    map[int64]string // observation id -> model of stored embedding
	deleteCalls   int
	missingLimits []int
}

func newFakeStore(n int, project string) *fakeStore {
	fs := &fakeStore{embeddings: map[int64]string{}}
	for i := 1; i <= n; i++ {
		fs.obs = append(fs.obs, fakeObs{
			id:      int64(i),
			title:   fmt.Sprintf("title-%d", i),
			content: fmt.Sprintf("content-%d", i),
			project: project,
		})
	}
	return fs
}

func (f *fakeStore) MissingEmbeddings(_ context.Context, project, model string, limit int) ([]store.Observation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.missingLimits = append(f.missingLimits, limit)

	var out []store.Observation
	for _, o := range f.obs {
		if project != "" && o.project != project {
			continue
		}
		if m, ok := f.embeddings[o.id]; ok && m == model {
			continue
		}
		out = append(out, store.Observation{ID: o.id, Title: o.title, Content: o.content, Project: o.project})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (f *fakeStore) UpsertEmbedding(_ context.Context, obsID int64, model string, _ []float32) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.embeddings[obsID] = model
	return nil
}

func (f *fakeStore) EmbeddingCoverage(_ context.Context, project, model string) (have, total int, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, o := range f.obs {
		if project != "" && o.project != project {
			continue
		}
		total++
		if m, ok := f.embeddings[o.id]; ok && m == model {
			have++
		}
	}
	return have, total, nil
}

func (f *fakeStore) DeleteAllEmbeddings(_ context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteCalls++
	n := int64(len(f.embeddings))
	f.embeddings = map[int64]string{}
	return n, nil
}

// ─── fake Embedder ────────────────────────────────────────────────────────────

// fakeEmbedder calls embedFn for every Embed call, passing a 1-based call
// counter and the requested text so tests can script failures precisely
// (e.g. "fail the first two calls for text X, then succeed").
type fakeEmbedder struct {
	mu      sync.Mutex
	model   string
	calls   int
	embedFn func(callNum int, text string) ([]float32, error)
}

func (f *fakeEmbedder) Model() string { return f.model }

func (f *fakeEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.calls++
	n := f.calls
	f.mu.Unlock()
	return f.embedFn(n, text)
}

func (f *fakeEmbedder) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// alwaysOK returns a fakeEmbedder that succeeds on every call.
func alwaysOK(model string) *fakeEmbedder {
	return &fakeEmbedder{
		model: model,
		embedFn: func(int, string) ([]float32, error) {
			return []float32{0.1, 0.2, 0.3}, nil
		},
	}
}

// noSleep is a Sleep replacement that never actually waits; it just honours
// ctx cancellation, keeping tests fast regardless of configured backoff.
func noSleep(ctx context.Context, _ time.Duration) error {
	return ctx.Err()
}

// drain collects every event from ch until it is closed.
func drain(ch <-chan embedjob.Event) []embedjob.Event {
	var out []embedjob.Event
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}

// ─── 1. Complete run ──────────────────────────────────────────────────────────

func TestRun_CompleteRun(t *testing.T) {
	fs := newFakeStore(5, "proj-a")
	fe := alwaysOK("fake-model")

	ch := embedjob.Run(context.Background(), fs, fe, nil, embedjob.Config{
		Project: "proj-a",
		Batch:   2,
		Sleep:   noSleep,
	})
	events := drain(ch)

	last := events[len(events)-1]
	if last.Type != embedjob.EventDone {
		t.Fatalf("last event type = %v, want EventDone", last.Type)
	}
	if last.Summary == nil {
		t.Fatal("EventDone.Summary is nil")
	}
	if last.Summary.Status != embedjob.StatusComplete {
		t.Errorf("Status = %v, want StatusComplete", last.Summary.Status)
	}
	if last.Summary.Done != 5 || last.Summary.Total != 5 {
		t.Errorf("Done/Total = %d/%d, want 5/5", last.Summary.Done, last.Summary.Total)
	}
	if last.Summary.Failed != 0 {
		t.Errorf("Failed = %d, want 0", last.Summary.Failed)
	}
}

// ─── 2. Retry then success ────────────────────────────────────────────────────

func TestRun_RetryThenSuccess(t *testing.T) {
	fs := newFakeStore(1, "proj-b")
	fe := &fakeEmbedder{
		model: "fake-model",
		embedFn: func(callNum int, _ string) ([]float32, error) {
			if callNum == 1 {
				return nil, errors.New("transient failure")
			}
			return []float32{1, 2, 3}, nil
		},
	}

	var sleepCalls int
	sleepFn := func(ctx context.Context, d time.Duration) error {
		sleepCalls++
		return ctx.Err()
	}

	ch := embedjob.Run(context.Background(), fs, fe, nil, embedjob.Config{
		Project: "proj-b",
		Batch:   10,
		Retries: 1, // 1 retry => up to 2 attempts total
		Sleep:   sleepFn,
	})
	events := drain(ch)

	var itemEvents []embedjob.Event
	for _, ev := range events {
		if ev.Type == embedjob.EventItem {
			itemEvents = append(itemEvents, ev)
		}
	}
	if len(itemEvents) != 1 {
		t.Fatalf("got %d EventItem events, want 1", len(itemEvents))
	}
	item := itemEvents[0]
	if item.Err != nil {
		t.Errorf("item.Err = %v, want nil (should have succeeded on retry)", item.Err)
	}
	if item.Attempt != 2 {
		t.Errorf("item.Attempt = %d, want 2", item.Attempt)
	}
	if sleepCalls != 1 {
		t.Errorf("sleepCalls = %d, want 1 (one backoff between attempt 1 and 2)", sleepCalls)
	}

	last := events[len(events)-1]
	if last.Summary.Status != embedjob.StatusComplete {
		t.Errorf("Status = %v, want StatusComplete", last.Summary.Status)
	}
}

// ─── 3. Item exhausts retries — Partial, not endlessly refetched ────────────

func TestRun_ItemExhaustsRetries_PartialNotRefetched(t *testing.T) {
	fs := newFakeStore(3, "proj-c") // ids 1,2,3
	fe := &fakeEmbedder{
		model: "fake-model",
		embedFn: func(_ int, text string) ([]float32, error) {
			if strings.HasPrefix(text, "title-1\n") {
				return nil, errors.New("permanently broken")
			}
			return []float32{1, 2, 3}, nil
		},
	}

	ch := embedjob.Run(context.Background(), fs, fe, nil, embedjob.Config{
		Project:                "proj-c",
		Batch:                  2,
		Retries:                1,
		MaxConsecutiveFailures: 10,
		Sleep:                  noSleep,
	})
	events := drain(ch)

	var failedItemEvents int
	for _, ev := range events {
		if ev.Type == embedjob.EventItem && ev.Err != nil {
			failedItemEvents++
			if ev.ID != 1 {
				t.Errorf("failed item id = %d, want 1", ev.ID)
			}
			if ev.Attempt != 2 {
				t.Errorf("failed item attempt = %d, want 2 (1+Retries)", ev.Attempt)
			}
		}
	}
	if failedItemEvents != 1 {
		t.Fatalf("item id=1 should fail exactly once (not be endlessly refetched), got %d failure events", failedItemEvents)
	}

	last := events[len(events)-1]
	if last.Summary.Status != embedjob.StatusPartial {
		t.Errorf("Status = %v, want StatusPartial", last.Summary.Status)
	}
	if last.Summary.Failed != 1 {
		t.Errorf("Failed = %d, want 1", last.Summary.Failed)
	}
	if last.Summary.Done != 2 {
		t.Errorf("Done = %d, want 2", last.Summary.Done)
	}

	// Bound on embedder calls proves the failing id wasn't refetched forever:
	// 2 attempts for id=1, plus 1 attempt each for ids 2 and 3.
	if got, want := fe.callCount(), 4; got != want {
		t.Errorf("embedder called %d times, want %d (no infinite refetch loop)", got, want)
	}
}

// ─── 4. Consecutive failures abort the job ───────────────────────────────────

func TestRun_ConsecutiveFailuresAborted(t *testing.T) {
	fs := newFakeStore(10, "proj-d")
	fe := &fakeEmbedder{
		model: "fake-model",
		embedFn: func(int, string) ([]float32, error) {
			return nil, errors.New("boom")
		},
	}

	ch := embedjob.Run(context.Background(), fs, fe, nil, embedjob.Config{
		Project:                "proj-d",
		Batch:                  10,
		Retries:                0, // 1 attempt per item, fail fast
		MaxConsecutiveFailures: 3,
		Sleep:                  noSleep,
	})
	events := drain(ch)

	last := events[len(events)-1]
	if last.Summary.Status != embedjob.StatusAborted {
		t.Errorf("Status = %v, want StatusAborted", last.Summary.Status)
	}
	if last.Summary.LastErr == nil || !strings.Contains(last.Summary.LastErr.Error(), "consecutive failures") {
		t.Errorf("LastErr = %v, want mention of 'consecutive failures'", last.Summary.LastErr)
	}
	if last.Summary.Failed != 3 {
		t.Errorf("Failed = %d, want 3 (aborts right at the threshold)", last.Summary.Failed)
	}

	// Retries: 0 must mean a single attempt per item (no retries), not the
	// package default. 3 items processed before the abort threshold, 1
	// embed call each.
	if got, want := fe.callCount(), 3; got != want {
		t.Errorf("embedder called %d times, want %d (Retries: 0 must mean a single attempt per item, not the default)", got, want)
	}
}

// ─── 4b. Context cancelled during the last (final) attempt ─────────────────

// TestRun_CtxCancelledDuringLastAttempt_StoppedNotFailed guards against a
// specific bug: if the parent ctx is cancelled while embedder.Embed is
// running on the LAST attempt (no more retries left), attemptEmbed must
// still report "stopped", not "exhausted its retries". Otherwise the item
// gets recorded as an ordinary failure (bumping Failed/consecutiveFailures
// and adding it to the failed-id set) even though the real cause was
// cancellation, and the job can even reach StatusAborted with a
// "consecutive failures: context canceled" message instead of the correct
// StatusStopped.
func TestRun_CtxCancelledDuringLastAttempt_StoppedNotFailed(t *testing.T) {
	fs := newFakeStore(1, "proj-cancel-last")
	ctx, cancel := context.WithCancel(context.Background())

	fe := &fakeEmbedder{
		model: "fake-model",
		embedFn: func(callNum int, _ string) ([]float32, error) {
			if callNum == 2 {
				// This is the last attempt (Retries: 1 => 1+1 = 2 max
				// attempts): cancel the parent ctx from inside the call,
				// simulating the embedder observing ctx cancellation and
				// returning while the outer ctx is already Done.
				cancel()
			}
			return nil, errors.New("boom")
		},
	}

	ch := embedjob.Run(ctx, fs, fe, nil, embedjob.Config{
		Project: "proj-cancel-last",
		Batch:   10,
		Retries: 1,
		Sleep:   noSleep,
	})
	events := drain(ch)

	for _, ev := range events {
		if ev.Type == embedjob.EventItem {
			t.Errorf("no EventItem should be recorded when the last attempt is cut short by ctx cancellation, got %+v", ev)
		}
	}

	last := events[len(events)-1]
	if last.Summary.Status != embedjob.StatusStopped {
		t.Errorf("Status = %v, want StatusStopped", last.Summary.Status)
	}
	if last.Summary.Failed != 0 {
		t.Errorf("Failed = %d, want 0 (ctx cancellation on the last attempt must not count as an item failure)", last.Summary.Failed)
	}
	if !errors.Is(last.Summary.LastErr, context.Canceled) {
		t.Errorf("LastErr = %v, want context.Canceled", last.Summary.LastErr)
	}
}

// ─── 5. Context cancellation mid-run ─────────────────────────────────────────

func TestRun_CtxCancelMidRun_Stopped(t *testing.T) {
	fs := newFakeStore(5, "proj-e")
	ctx, cancel := context.WithCancel(context.Background())

	fe := &fakeEmbedder{
		model: "fake-model",
		embedFn: func(callNum int, _ string) ([]float32, error) {
			if callNum == 1 {
				// Cancel right after the first item is embedded so the
				// second item observes ctx.Err() at the top of the loop.
				cancel()
			}
			return []float32{1, 2, 3}, nil
		},
	}

	ch := embedjob.Run(ctx, fs, fe, nil, embedjob.Config{
		Project: "proj-e",
		Batch:   10,
		Sleep:   noSleep,
	})
	events := drain(ch)

	last := events[len(events)-1]
	if last.Type != embedjob.EventDone {
		t.Fatalf("last event type = %v, want EventDone", last.Type)
	}
	if last.Summary.Status != embedjob.StatusStopped {
		t.Errorf("Status = %v, want StatusStopped", last.Summary.Status)
	}
	if !errors.Is(last.Summary.LastErr, context.Canceled) {
		t.Errorf("LastErr = %v, want context.Canceled", last.Summary.LastErr)
	}
}

// ─── 6. Regenerate deletes all embeddings first ──────────────────────────────

func TestRun_Regenerate_DeletesAllEmbeddingsFirst(t *testing.T) {
	fs := newFakeStore(2, "proj-f")
	// Pre-seed an embedding under a stale model so DeleteAllEmbeddings has
	// something to remove.
	fs.embeddings[1] = "stale-model"

	fe := alwaysOK("fake-model")

	ch := embedjob.Run(context.Background(), fs, fe, nil, embedjob.Config{
		Kind:    embedjob.KindRegenerate,
		Project: "proj-f",
		Batch:   10,
		Sleep:   noSleep,
	})
	events := drain(ch)

	if fs.deleteCalls != 1 {
		t.Errorf("DeleteAllEmbeddings called %d times, want 1", fs.deleteCalls)
	}
	last := events[len(events)-1]
	if last.Summary.Status != embedjob.StatusComplete {
		t.Errorf("Status = %v, want StatusComplete", last.Summary.Status)
	}
	if last.Summary.Done != 2 {
		t.Errorf("Done = %d, want 2 (re-embedded after wipe)", last.Summary.Done)
	}
}

// ─── 7. Warm-up failure aborts before any item ───────────────────────────────

func TestRun_WarmUpFailure_Aborted(t *testing.T) {
	fs := newFakeStore(3, "proj-g")
	fe := &fakeEmbedder{
		model: "fake-model",
		embedFn: func(_ int, text string) ([]float32, error) {
			if text == "warm-up" {
				return nil, errors.New("ollama unreachable")
			}
			return []float32{1, 2, 3}, nil
		},
	}

	ch := embedjob.Run(context.Background(), fs, fe, nil, embedjob.Config{
		Project: "proj-g",
		Batch:   10,
		WarmUp:  true,
		Sleep:   noSleep,
	})
	events := drain(ch)

	for _, ev := range events {
		if ev.Type == embedjob.EventItem {
			t.Fatalf("no items should be processed after a warm-up failure, got EventItem for id=%d", ev.ID)
		}
	}

	last := events[len(events)-1]
	if last.Summary.Status != embedjob.StatusAborted {
		t.Errorf("Status = %v, want StatusAborted", last.Summary.Status)
	}
	if last.Summary.LastErr == nil || !strings.Contains(last.Summary.LastErr.Error(), "warm-up failed") {
		t.Errorf("LastErr = %v, want mention of 'warm-up failed'", last.Summary.LastErr)
	}
}

// ─── 8. Every run ends with exactly one EventDone, channel closes ───────────

func TestRun_EveryRunEndsWithExactlyOneEventDone(t *testing.T) {
	fs := newFakeStore(4, "proj-h")
	fe := alwaysOK("fake-model")

	ch := embedjob.Run(context.Background(), fs, fe, nil, embedjob.Config{
		Project: "proj-h",
		Batch:   2,
		Sleep:   noSleep,
	})
	events := drain(ch)

	var doneCount int
	for _, ev := range events {
		if ev.Type == embedjob.EventDone {
			doneCount++
		}
	}
	if doneCount != 1 {
		t.Errorf("EventDone count = %d, want 1", doneCount)
	}
	if events[len(events)-1].Type != embedjob.EventDone {
		t.Error("EventDone should be the last event")
	}

	// The channel must be closed: a second receive on a drained, closed
	// channel returns the zero Event and ok == false.
	_, ok := <-ch
	if ok {
		t.Error("channel should be closed after EventDone")
	}
}

// ─── 9. Counters are monotonic across events ────────────────────────────────

func TestRun_CountersAreMonotonic(t *testing.T) {
	fs := newFakeStore(6, "proj-i")
	fe := &fakeEmbedder{
		model: "fake-model",
		embedFn: func(_ int, text string) ([]float32, error) {
			if strings.HasPrefix(text, "title-2\n") || strings.HasPrefix(text, "title-4\n") {
				return nil, errors.New("fails")
			}
			return []float32{1, 2, 3}, nil
		},
	}

	ch := embedjob.Run(context.Background(), fs, fe, nil, embedjob.Config{
		Project:                "proj-i",
		Batch:                  3,
		Retries:                0,
		MaxConsecutiveFailures: 10,
		Sleep:                  noSleep,
	})
	events := drain(ch)

	var prevDone, prevFailed int
	for i, ev := range events {
		if ev.Done < prevDone {
			t.Errorf("event %d: Done decreased: %d < %d", i, ev.Done, prevDone)
		}
		if ev.Failed < prevFailed {
			t.Errorf("event %d: Failed decreased: %d < %d", i, ev.Failed, prevFailed)
		}
		prevDone, prevFailed = ev.Done, ev.Failed
	}
}
