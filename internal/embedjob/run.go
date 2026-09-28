package embedjob

import (
	"context"
	"fmt"
	"time"

	"github.com/arodriguezp2003/ion-mem/internal/embed"
	"github.com/arodriguezp2003/ion-mem/internal/joblog"
	"github.com/arodriguezp2003/ion-mem/internal/store"
)

// Run starts the job in a goroutine and returns a channel that receives
// progress Events. The channel is buffered (eventBufferSize) and is CLOSED
// when the job ends; every run emits exactly one EventDone as its final
// event before the channel closes.
//
// The channel is buffered so the producer goroutine never blocks forever on
// send: callers are expected to keep draining the channel (e.g. with a
// simple `for ev := range ch`) until it closes. Run does not itself select
// on ctx while sending, so a consumer that stops draining before the channel
// closes can still stall the goroutine — this is an accepted tradeoff given
// the documented "always drain" contract. The consumer MUST drain the
// channel until it closes; a stalled consumer blocks the job goroutine
// forever once the buffer fills.
//
// Run does not recover panics from st or embedder. A panic inside either
// will propagate up through the job goroutine and crash the whole host
// process (the CLI process, or the TUI process once Phase 3 wires this in)
// rather than being reported as a failed Event or Summary. Callers that pass
// in third-party or otherwise untrusted Store/Embedder implementations are
// responsible for making those implementations panic-safe themselves.
func Run(ctx context.Context, st Store, embedder embed.Embedder, lg *joblog.Logger, cfg Config) <-chan Event {
	cfg = cfg.withDefaults()
	if lg == nil {
		lg = joblog.Nop()
	}

	events := make(chan Event, eventBufferSize)
	go func() {
		defer close(events)
		runJob(ctx, st, embedder, lg, cfg, events)
	}()
	return events
}

// jobState carries the running counters and last-seen error across the
// helper functions below, and centralises event emission so every Event gets
// consistent Done/Failed/Total counters.
type jobState struct {
	done, failed, total int
	lastErr             error
	events              chan<- Event
}

func (js *jobState) emit(ev Event) {
	ev.Done = js.done
	ev.Failed = js.failed
	ev.Total = js.total
	js.events <- ev
}

// runJob is the synchronous body of a job run; Run() invokes it in a
// goroutine and closes the channel when it returns.
func runJob(ctx context.Context, st Store, embedder embed.Embedder, lg *joblog.Logger, cfg Config, events chan<- Event) {
	start := time.Now()
	model := embedder.Model()
	js := &jobState{events: events}

	lg.Info("embedjob start kind=%s project=%q model=%q batch=%d item_timeout=%s retries=%d max_consecutive_failures=%d log=%q",
		cfg.Kind, cfg.Project, model, cfg.Batch, cfg.ItemTimeout, cfg.Retries, cfg.MaxConsecutiveFailures, lg.Path())

	js.emit(Event{Type: EventStarted})

	if cfg.Kind == KindRegenerate {
		n, err := st.DeleteAllEmbeddings(ctx)
		if err != nil {
			js.lastErr = fmt.Errorf("embedjob: delete all embeddings: %w", err)
			finish(js, lg, start, StatusAborted)
			return
		}
		lg.Info("regenerate: deleted %d existing embeddings", n)
	}

	if cfg.WarmUp {
		warmCtx, cancel := context.WithTimeout(ctx, warmUpTimeout)
		wstart := time.Now()
		_, err := embedder.Embed(warmCtx, warmUpText)
		welapsed := time.Since(wstart)
		cancel()
		if err != nil {
			js.lastErr = fmt.Errorf("embedjob: warm-up failed: %w", err)
			lg.Info("warm-up failed after %s: %v", welapsed, err)
			finish(js, lg, start, StatusAborted)
			return
		}
		lg.Info("warm-up ok duration=%s", welapsed)
		js.emit(Event{Type: EventWarmedUp, Duration: welapsed})
	}

	if have, total, err := st.EmbeddingCoverage(ctx, cfg.Project, model); err == nil {
		js.done, js.total = have, total
	}

	status := runLoop(ctx, st, embedder, lg, cfg, model, js)

	// Authoritative final counts: re-query coverage rather than trusting the
	// running counters, which could drift from reality (e.g. concurrent
	// writers, or an upsert that "succeeded" per our bookkeeping but the
	// underlying row was later touched by something else).
	if have, total, err := st.EmbeddingCoverage(ctx, cfg.Project, model); err == nil {
		js.done, js.total = have, total
	}

	if status == StatusComplete && js.failed > 0 {
		status = StatusPartial
	}
	finish(js, lg, start, status)
}

// runLoop fetches and processes pages of missing observations until nothing
// remains, the context is cancelled, or the job aborts. It returns the
// terminal status (StatusComplete unless overridden by the caller for the
// Partial case).
func runLoop(ctx context.Context, st Store, embedder embed.Embedder, lg *joblog.Logger, cfg Config, model string, js *jobState) Status {
	failedIDs := make(map[int64]bool)
	consecutiveFailures := 0

	for {
		if err := ctx.Err(); err != nil {
			js.lastErr = err
			return StatusStopped
		}

		limit := cfg.Batch + len(failedIDs)
		page, err := st.MissingEmbeddings(ctx, cfg.Project, model, limit)
		if err != nil {
			js.lastErr = fmt.Errorf("embedjob: fetch missing: %w", err)
			return StatusAborted
		}

		var pending []store.Observation
		for _, obs := range page {
			if failedIDs[obs.ID] {
				continue
			}
			pending = append(pending, obs)
		}
		if len(pending) == 0 {
			return StatusComplete
		}

		for _, obs := range pending {
			if err := ctx.Err(); err != nil {
				js.lastErr = err
				return StatusStopped
			}

			status, stop := processItem(ctx, st, embedder, lg, cfg, model, obs, js, failedIDs, &consecutiveFailures)
			if stop {
				return status
			}
		}
	}
}

// processItem embeds and upserts a single observation, updating js and
// failedIDs/consecutiveFailures as a side effect. It returns (status, true)
// when the caller should stop the whole run (StatusStopped or
// StatusAborted); otherwise it returns (_, false) and the loop continues.
func processItem(
	ctx context.Context,
	st Store,
	embedder embed.Embedder,
	lg *joblog.Logger,
	cfg Config,
	model string,
	obs store.Observation,
	js *jobState,
	failedIDs map[int64]bool,
	consecutiveFailures *int,
) (Status, bool) {
	text := obs.Title + "\n" + obs.Content

	vec, attempts, elapsed, embedErr, stopped := attemptEmbed(ctx, embedder, text, cfg)
	if stopped {
		js.lastErr = embedErr
		return StatusStopped, true
	}

	if embedErr == nil {
		if upsertErr := st.UpsertEmbedding(ctx, obs.ID, model, vec); upsertErr != nil {
			embedErr = upsertErr
		}
	}

	if embedErr != nil {
		js.failed++
		js.lastErr = embedErr
		failedIDs[obs.ID] = true
		*consecutiveFailures++
		lg.Info("ERROR item id=%d title=%q attempts=%d err=%v", obs.ID, obs.Title, attempts, embedErr)
		js.emit(Event{Type: EventItem, ID: obs.ID, Title: obs.Title, Attempt: attempts, Err: embedErr})

		if *consecutiveFailures >= cfg.MaxConsecutiveFailures {
			js.lastErr = fmt.Errorf("embedjob: aborted after %d consecutive failures: %w", *consecutiveFailures, embedErr)
			return StatusAborted, true
		}
		return StatusComplete, false
	}

	js.done++
	*consecutiveFailures = 0
	lg.Debug("embed ok id=%d title=%q duration=%s dims=%d", obs.ID, obs.Title, elapsed, len(vec))
	js.emit(Event{Type: EventItem, ID: obs.ID, Title: obs.Title, Duration: elapsed, Attempt: attempts})
	return StatusComplete, false
}

// attemptEmbed calls embedder.Embed up to 1+cfg.Retries times, sleeping
// cfg.Sleep between attempts. It returns the vector and attempt count on
// success, or the last error and attempt count when every attempt failed.
// stopped is true when ctx was cancelled (either before an attempt or during
// the backoff sleep), signalling the caller to abandon the whole run rather
// than treat this as an ordinary item failure.
func attemptEmbed(ctx context.Context, embedder embed.Embedder, text string, cfg Config) (vec []float32, attempts int, elapsed time.Duration, err error, stopped bool) {
	maxAttempts := 1 + cfg.Retries

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if cerr := ctx.Err(); cerr != nil {
			return nil, attempt, 0, cerr, true
		}

		itemCtx, cancel := context.WithTimeout(ctx, cfg.ItemTimeout)
		start := time.Now()
		v, embedErr := embedder.Embed(itemCtx, text)
		d := time.Since(start)
		cancel()

		if embedErr == nil {
			return v, attempt, d, nil, false
		}
		err = embedErr

		if attempt == maxAttempts {
			break
		}

		backoff := backoffFor(cfg.Backoff, attempt-1)
		if serr := cfg.Sleep(ctx, backoff); serr != nil {
			return nil, attempt, 0, serr, true
		}
	}

	// The last attempt failed with no retries left. Before reporting an
	// ordinary item failure, check whether the parent ctx was cancelled
	// during that final embedder.Embed call: itemCtx is derived from ctx,
	// so a well-behaved embedder observes the same cancellation and its
	// returned error is really "the job was stopped", not "this item is
	// broken". Recording it as an item failure would be wrong on two
	// counts: it would count against MaxConsecutiveFailures (possibly
	// producing a misleading "aborted: consecutive failures: context
	// canceled"), and it would add the item to the failed set even though
	// it was never actually given a real chance to fail.
	if cerr := ctx.Err(); cerr != nil {
		return nil, maxAttempts, 0, cerr, true
	}
	return nil, maxAttempts, 0, err, false
}

// finish builds the Summary, logs it, and emits the terminal EventDone.
func finish(js *jobState, lg *joblog.Logger, start time.Time, status Status) {
	summary := &Summary{
		Status:  status,
		Done:    js.done,
		Failed:  js.failed,
		Total:   js.total,
		Elapsed: time.Since(start),
		LastErr: js.lastErr,
		LogPath: lg.Path(),
	}
	lg.Info("embedjob done status=%s done=%d/%d failed=%d elapsed=%s last_err=%v",
		status, summary.Done, summary.Total, summary.Failed, summary.Elapsed, summary.LastErr)
	js.emit(Event{Type: EventDone, Summary: summary})
}
