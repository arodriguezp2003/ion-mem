package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arodriguezp2003/ion-mem/internal/store"
)

// noopLauncher is an ollamaLauncher that never spawns a real process —
// used by every test in this file so no test ever shells out to `ollama`.
func noopLauncher(_ string) error { return nil }

func failingLauncher(_ string) error { return errors.New("launcher: exec not found") }

// ─── flag parsing ─────────────────────────────────────────────────────────────

func TestParseDoctorFlags_Defaults(t *testing.T) {
	cfg, err := parseDoctorFlags(nil, fakeHome)
	if err != nil {
		t.Fatalf("parseDoctorFlags: %v", err)
	}
	if cfg.jsonOut {
		t.Error("jsonOut should default to false")
	}
	if cfg.autostart {
		t.Error("autostart should default to false")
	}
	if cfg.wait != 5*time.Second {
		t.Errorf("wait = %v, want 5s", cfg.wait)
	}
}

func TestParseDoctorFlags_AllFlags(t *testing.T) {
	cfg, err := parseDoctorFlags([]string{"--json", "--autostart", "--wait=2s", "--timeout=5s", "--data-dir=/tmp/x"}, fakeHome)
	if err != nil {
		t.Fatalf("parseDoctorFlags: %v", err)
	}
	if !cfg.jsonOut || !cfg.autostart {
		t.Errorf("cfg = %+v, want jsonOut=true autostart=true", cfg)
	}
	if cfg.wait != 2*time.Second {
		t.Errorf("wait = %v, want 2s", cfg.wait)
	}
	if cfg.dataDir != "/tmp/x" {
		t.Errorf("dataDir = %q, want /tmp/x", cfg.dataDir)
	}
	if !cfg.timeoutSet {
		t.Error("timeoutSet should be true when --timeout is explicitly passed")
	}
	if cfg.timeout != 5*time.Second {
		t.Errorf("timeout = %v, want 5s", cfg.timeout)
	}
}

// TestParseDoctorFlags_TimeoutNotPassed_DefaultsButUnset verifies the
// fs.Visit-based distinction (mirroring backfillConfig.verbose): the flag's
// default value is 8s (shown in --help), but timeoutSet stays false when
// the user never passes --timeout, which is what tells buildDoctorReport to
// keep the historical fixed ping=2s/probe=10s timeouts (SHOULD #3).
func TestParseDoctorFlags_TimeoutNotPassed_DefaultsButUnset(t *testing.T) {
	cfg, err := parseDoctorFlags(nil, fakeHome)
	if err != nil {
		t.Fatalf("parseDoctorFlags: %v", err)
	}
	if cfg.timeoutSet {
		t.Error("timeoutSet should be false when --timeout was never passed")
	}
	if cfg.timeout != 8*time.Second {
		t.Errorf("timeout default = %v, want 8s", cfg.timeout)
	}
}

// ─── wall-clock budget (review fix: remove external `timeout` dependency) ────

// TestDoctorBudget_NoExplicitTimeout_KeepsHistoricalFixedTimeouts verifies
// SHOULD #3: standalone runs (no --timeout) keep the pre-existing fixed
// ping=2s/probe=10s timeouts, with no overall deadline.
func TestDoctorBudget_NoExplicitTimeout_KeepsHistoricalFixedTimeouts(t *testing.T) {
	b := newDoctorBudget(context.Background(), doctorConfig{timeoutSet: false, timeout: 8 * time.Second})
	defer b.cancel()

	if got := b.pingTimeout(); got != doctorPingTimeout {
		t.Errorf("pingTimeout() = %v, want %v (unbounded mode)", got, doctorPingTimeout)
	}
	if got := b.probeTimeout(); got != doctorProbeTimeout {
		t.Errorf("probeTimeout() = %v, want %v (unbounded mode)", got, doctorProbeTimeout)
	}
	if _, hasDeadline := b.ctx.Deadline(); hasDeadline {
		t.Error("ctx should have no deadline when --timeout was not explicitly passed")
	}
}

// TestDoctorBudget_ExplicitTimeout_DerivesPerCheckCaps verifies BLOCKING #2:
// with an explicit --timeout, ping is capped at min(2s, remaining) and
// probe at min(4s, remaining) — here with plenty of remaining budget, both
// caps should equal their fixed ceiling (2s, 4s).
func TestDoctorBudget_ExplicitTimeout_DerivesPerCheckCaps(t *testing.T) {
	b := newDoctorBudget(context.Background(), doctorConfig{timeoutSet: true, timeout: 5 * time.Second})
	defer b.cancel()

	if got := b.pingTimeout(); got <= 0 || got > doctorPingTimeout {
		t.Errorf("pingTimeout() = %v, want (0, %v]", got, doctorPingTimeout)
	}
	if got := b.probeTimeout(); got <= 0 || got > explicitProbeCap {
		t.Errorf("probeTimeout() = %v, want (0, %v]", got, explicitProbeCap)
	}
	if _, hasDeadline := b.ctx.Deadline(); !hasDeadline {
		t.Error("ctx should carry a deadline when --timeout was explicitly passed")
	}
}

// TestDoctorBudget_RemainingBudgetSmallerThanCap_Shrinks verifies that once
// the overall budget has nearly elapsed, the derived per-check timeouts
// shrink below their fixed caps (2s/4s) rather than overrunning it.
func TestDoctorBudget_RemainingBudgetSmallerThanCap_Shrinks(t *testing.T) {
	b := newDoctorBudget(context.Background(), doctorConfig{timeoutSet: true, timeout: 20 * time.Millisecond})
	defer b.cancel()
	time.Sleep(25 * time.Millisecond) // let the budget fully elapse

	if got := b.pingTimeout(); got != 0 {
		t.Errorf("pingTimeout() after budget elapsed = %v, want 0", got)
	}
	if got := b.probeTimeout(); got != 0 {
		t.Errorf("probeTimeout() after budget elapsed = %v, want 0", got)
	}
}

// TestRunDoctor_ExplicitTimeout_BoundsSlowOllama verifies end-to-end that a
// small --timeout actually bounds a slow Ollama server: without the fix,
// Ping/HasModel/ProbeEmbed would use their fixed 2s/2s/10s timeouts
// regardless, and this run would take at least 300ms (the server's
// artificial per-request delay) times the number of calls; with the budget
// derivation, the whole call returns near --timeout's bound.
func TestRunDoctor_ExplicitTimeout_BoundsSlowOllama(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		http.NotFound(w, r) // never actually answers usefully; only latency matters here
	}))
	defer srv.Close()

	dir := doctorTestStore(t)
	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	if err := st.SetSetting(context.Background(), store.SettingOllamaURL, srv.URL); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	_ = st.Close()

	start := time.Now()
	var sb strings.Builder
	_ = runDoctorWithLauncher([]string{"--data-dir=" + dir, "--timeout=100ms"}, &sb, fakeHome, noopLauncher)
	elapsed := time.Since(start)

	// Generous upper bound: the budget is 100ms total: if per-check timeouts
	// were NOT derived from it, Ping alone would block for its old fixed 2s.
	if elapsed > 2*time.Second {
		t.Errorf("runDoctorWithLauncher with --timeout=100ms took %v, want well under 2s (old fixed ping timeout)", elapsed)
	}
}

// TestRunDoctor_Autostart_BudgetCutsPollingShort verifies that an explicit
// --timeout bounds --autostart's polling loop too, even when --wait is much
// larger: attemptAutostart must exit via ctx.Done(), not just its own wait
// deadline, so autostart can never by itself blow the overall budget.
func TestRunDoctor_Autostart_BudgetCutsPollingShort(t *testing.T) {
	dir := doctorTestStore(t)
	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	ctx := context.Background()
	if err := st.SetSetting(ctx, store.SettingOllamaURL, "http://127.0.0.1:1"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if err := st.SetSetting(ctx, store.SettingOllamaAutostart, "true"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	_ = st.Close()

	start := time.Now()
	var sb strings.Builder
	// --wait=10s would normally poll for up to 10s; --timeout=200ms must cut
	// that short.
	_ = runDoctorWithLauncher(
		[]string{"--data-dir=" + dir, "--autostart", "--wait=10s", "--timeout=200ms", "--json"},
		&sb, fakeHome, noopLauncher,
	)
	elapsed := time.Since(start)
	if elapsed > 3*time.Second {
		t.Errorf("runDoctorWithLauncher with --wait=10s --timeout=200ms took %v, want well under the 10s wait", elapsed)
	}

	var rep doctorReport
	if jerr := json.Unmarshal([]byte(sb.String()), &rep); jerr != nil {
		t.Fatalf("decode: %v", jerr)
	}
	if rep.AutostartResult == "" {
		t.Error("autostart_result should still be set when the budget cuts polling short")
	}
}

// ─── verdict resolution (table-driven) ────────────────────────────────────────

func TestResolveDoctorVerdict_TableDriven(t *testing.T) {
	cases := []struct {
		name          string
		rep           doctorReport
		needsVectors  bool
		wantVerdict   string
		wantEffective string
		wantHintSub   string // substring expected somewhere in hints; "" = don't check
		wantNoHintSub string // substring that must NOT appear in any hint; "" = don't check
	}{
		{
			name:          "lexical mode always ok",
			rep:           doctorReport{SearchMode: "lexical"},
			needsVectors:  false,
			wantVerdict:   "ok",
			wantEffective: "lexical",
		},
		{
			name:          "vector mode, embeddings disabled",
			rep:           doctorReport{SearchMode: "vector", EmbeddingsEnabled: false},
			needsVectors:  true,
			wantVerdict:   "degraded",
			wantEffective: "lexical",
			wantHintSub:   "embeddings.enabled true",
		},
		{
			name:          "vector mode, enabled but ollama unreachable",
			rep:           doctorReport{SearchMode: "vector", EmbeddingsEnabled: true, OllamaReachable: false},
			needsVectors:  true,
			wantVerdict:   "degraded",
			wantEffective: "lexical",
		},
		{
			name: "vector mode, reachable but model missing",
			rep: doctorReport{
				SearchMode: "vector", EmbeddingsEnabled: true,
				OllamaReachable: true, ModelPresent: false, Model: "bge-m3",
			},
			needsVectors:  true,
			wantVerdict:   "degraded",
			wantEffective: "lexical",
			wantHintSub:   "ollama pull bge-m3",
		},
		{
			name: "vector mode, fully satisfiable",
			rep: doctorReport{
				SearchMode: "vector", EmbeddingsEnabled: true,
				OllamaReachable: true, ModelPresent: true, ProbeOK: true,
				Coverage: doctorCoverage{Have: 10, Total: 10},
			},
			needsVectors:  true,
			wantVerdict:   "ok",
			wantEffective: "vector",
		},
		{
			name: "hybrid mode, fully satisfiable but coverage incomplete",
			rep: doctorReport{
				SearchMode: "hybrid", EmbeddingsEnabled: true,
				OllamaReachable: true, ModelPresent: true, ProbeOK: true,
				Coverage: doctorCoverage{Have: 3, Total: 10},
			},
			needsVectors:  true,
			wantVerdict:   "ok", // satisfiable; coverage gap is a hint, not a degrade
			wantEffective: "hybrid",
			wantHintSub:   "backfill-embeddings",
		},
		{
			name: "vector mode, model present but probe embed fails",
			rep: doctorReport{
				SearchMode: "vector", EmbeddingsEnabled: true,
				OllamaReachable: true, ModelPresent: true, ProbeOK: false,
				ProbeError: "unexpected status 500", Model: "bge-m3",
			},
			needsVectors:  true,
			wantVerdict:   "degraded",
			wantEffective: "lexical",
			wantHintSub:   "ollama embed probe failed",
		},
		{
			name: "vector mode, HasModel itself errored — no ollama-pull hint",
			rep: doctorReport{
				SearchMode: "vector", EmbeddingsEnabled: true,
				OllamaReachable: true, ModelPresent: false,
				ModelCheckError: "unexpected status 500", Model: "bge-m3",
			},
			needsVectors:  true,
			wantVerdict:   "degraded",
			wantEffective: "lexical",
			wantHintSub:   "model list",
			wantNoHintSub: "ollama pull",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			effective, verdict, hints := resolveDoctorVerdict(tc.rep, tc.needsVectors)
			if verdict != tc.wantVerdict {
				t.Errorf("verdict = %q, want %q", verdict, tc.wantVerdict)
			}
			if effective != tc.wantEffective {
				t.Errorf("effectiveMode = %q, want %q", effective, tc.wantEffective)
			}
			if tc.wantHintSub != "" {
				found := false
				for _, h := range hints {
					if strings.Contains(h, tc.wantHintSub) {
						found = true
					}
				}
				if !found {
					t.Errorf("hints = %v, want one containing %q", hints, tc.wantHintSub)
				}
			}
			if tc.wantNoHintSub != "" {
				for _, h := range hints {
					if strings.Contains(h, tc.wantNoHintSub) {
						t.Errorf("hints = %v, must NOT contain %q", hints, tc.wantNoHintSub)
					}
				}
			}
		})
	}
}

// ─── runDoctorWithLauncher against httptest Ollama ────────────────────────────

func doctorTestStore(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	if err := st.SetSetting(ctx, store.SettingEmbeddingsEnabled, "true"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if err := st.SetSetting(ctx, store.SettingEmbeddingsModel, "bge-m3"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	return dir
}

func TestRunDoctor_Reachable_ModelPresent_Ok(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]string{{"name": "bge-m3"}},
			})
		case "/api/embeddings":
			json.NewEncoder(w).Encode(map[string]any{"embedding": []float32{1, 2, 3}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dir := doctorTestStore(t)
	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	if err := st.SetSetting(context.Background(), store.SettingOllamaURL, srv.URL); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	_ = st.Close()

	var sb strings.Builder
	err = runDoctorWithLauncher([]string{"--data-dir=" + dir, "--json"}, &sb, fakeHome, noopLauncher)
	if err != nil {
		t.Fatalf("runDoctorWithLauncher: %v", err)
	}

	var rep doctorReport
	if err := json.Unmarshal([]byte(sb.String()), &rep); err != nil {
		t.Fatalf("decode report: %v\nraw: %s", err, sb.String())
	}
	if rep.Verdict != "ok" {
		t.Errorf("verdict = %q, want ok: %+v", rep.Verdict, rep)
	}
	if !rep.OllamaReachable || !rep.ModelPresent {
		t.Errorf("reachable=%v present=%v, want both true", rep.OllamaReachable, rep.ModelPresent)
	}
	if rep.ProbeDims != 3 {
		t.Errorf("probe_dims = %d, want 3", rep.ProbeDims)
	}
}

func TestRunDoctor_Unreachable_Degraded_ExitCode1(t *testing.T) {
	dir := doctorTestStore(t)
	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	if err := st.SetSetting(context.Background(), store.SettingOllamaURL, "http://127.0.0.1:1"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	_ = st.Close()

	var sb strings.Builder
	err = runDoctorWithLauncher([]string{"--data-dir=" + dir}, &sb, fakeHome, noopLauncher)
	if err == nil {
		t.Fatal("expected a degraded error")
	}
	ec, ok := err.(interface{ ExitCode() int })
	if !ok {
		t.Fatalf("error %v does not implement ExitCode()", err)
	}
	if ec.ExitCode() != 1 {
		t.Errorf("ExitCode() = %d, want 1", ec.ExitCode())
	}
	if !strings.Contains(sb.String(), "degraded") {
		t.Errorf("text output missing 'degraded': %s", sb.String())
	}
}

func TestRunDoctor_ModelMissing_Degraded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]string{{"name": "some-other-model"}},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	dir := doctorTestStore(t)
	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	if err := st.SetSetting(context.Background(), store.SettingOllamaURL, srv.URL); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	_ = st.Close()

	var sb strings.Builder
	err = runDoctorWithLauncher([]string{"--data-dir=" + dir, "--json"}, &sb, fakeHome, noopLauncher)
	if err == nil {
		t.Fatal("expected a degraded error")
	}
	var rep doctorReport
	if jerr := json.Unmarshal([]byte(sb.String()), &rep); jerr != nil {
		t.Fatalf("decode report: %v", jerr)
	}
	if rep.Verdict != "degraded" || rep.ModelPresent {
		t.Errorf("rep = %+v, want verdict=degraded model_present=false", rep)
	}
	found := false
	for _, h := range rep.Hints {
		if strings.Contains(h, "ollama pull bge-m3") {
			found = true
		}
	}
	if !found {
		t.Errorf("hints = %v, want an 'ollama pull bge-m3' hint", rep.Hints)
	}
}

func TestRunDoctor_StoreUnreadable_Down_ExitCode2(t *testing.T) {
	var sb strings.Builder
	err := runDoctorWithLauncher([]string{"--data-dir=relative-path"}, &sb, fakeHome, noopLauncher)
	if err == nil {
		t.Fatal("expected a down error for an unreadable store")
	}
	ec, ok := err.(interface{ ExitCode() int })
	if !ok {
		t.Fatalf("error %v does not implement ExitCode()", err)
	}
	if ec.ExitCode() != 2 {
		t.Errorf("ExitCode() = %d, want 2", ec.ExitCode())
	}
}

func TestRunDoctor_LexicalMode_NeverPingsOllama(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := st.SetSetting(context.Background(), store.SettingSearchMode, "lexical"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	// Deliberately unreachable URL: if doctor pinged it under lexical mode,
	// the call would hang/fail. It must not, since lexical needs no vectors.
	if err := st.SetSetting(context.Background(), store.SettingOllamaURL, "http://127.0.0.1:1"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	_ = st.Close()

	var sb strings.Builder
	err = runDoctorWithLauncher([]string{"--data-dir=" + dir}, &sb, fakeHome, noopLauncher)
	if err != nil {
		t.Fatalf("runDoctorWithLauncher: %v", err)
	}
	if !strings.Contains(sb.String(), "verdict:          ok") {
		t.Errorf("output = %q, want ok verdict", sb.String())
	}
}

// ─── --autostart (injected launcher; never spawns a real process) ────────────

func TestRunDoctor_Autostart_DisabledSetting_NeverAttempted(t *testing.T) {
	dir := doctorTestStore(t)
	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if err := st.SetSetting(context.Background(), store.SettingOllamaURL, "http://127.0.0.1:1"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	// ollama.autostart left unset (defaults to "false").
	_ = st.Close()

	launched := false
	launcher := func(_ string) error { launched = true; return nil }

	var sb strings.Builder
	_ = runDoctorWithLauncher([]string{"--data-dir=" + dir, "--autostart", "--json"}, &sb, fakeHome, launcher)
	if launched {
		t.Error("launcher must not be called when ollama.autostart setting is false")
	}
	var rep doctorReport
	if jerr := json.Unmarshal([]byte(sb.String()), &rep); jerr != nil {
		t.Fatalf("decode: %v", jerr)
	}
	if rep.AutostartAttempted {
		t.Error("autostart_attempted should be false")
	}
}

func TestRunDoctor_Autostart_EnabledAndUnreachable_Attempts(t *testing.T) {
	dir := doctorTestStore(t)
	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	ctx := context.Background()
	if err := st.SetSetting(ctx, store.SettingOllamaURL, "http://127.0.0.1:1"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if err := st.SetSetting(ctx, store.SettingOllamaAutostart, "true"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	_ = st.Close()

	launched := false
	launcher := func(_ string) error { launched = true; return nil }

	var sb strings.Builder
	_ = runDoctorWithLauncher([]string{"--data-dir=" + dir, "--autostart", "--wait=600ms", "--json"}, &sb, fakeHome, launcher)
	if !launched {
		t.Error("launcher should have been called (autostart=true, unreachable)")
	}
	var rep doctorReport
	if jerr := json.Unmarshal([]byte(sb.String()), &rep); jerr != nil {
		t.Fatalf("decode: %v", jerr)
	}
	if !rep.AutostartAttempted {
		t.Error("autostart_attempted should be true")
	}
	if rep.AutostartResult == "" {
		t.Error("autostart_result should be set")
	}
}

func TestRunDoctor_Autostart_LauncherFails_RecordsResult(t *testing.T) {
	dir := doctorTestStore(t)
	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	ctx := context.Background()
	if err := st.SetSetting(ctx, store.SettingOllamaURL, "http://127.0.0.1:1"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if err := st.SetSetting(ctx, store.SettingOllamaAutostart, "true"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	_ = st.Close()

	var sb strings.Builder
	_ = runDoctorWithLauncher([]string{"--data-dir=" + dir, "--autostart", "--json"}, &sb, fakeHome, failingLauncher)

	var rep doctorReport
	if jerr := json.Unmarshal([]byte(sb.String()), &rep); jerr != nil {
		t.Fatalf("decode: %v", jerr)
	}
	if !strings.Contains(rep.AutostartResult, "failed") {
		t.Errorf("autostart_result = %q, want it to mention the launch failure", rep.AutostartResult)
	}
}

// ─── probe/coverage/model-check error surfacing (review fix) ─────────────────

// TestRunDoctor_ProbeEmbedFails_Degraded verifies that a reachable Ollama
// with the model present, but a failing /api/embeddings call, is reported
// as degraded (not silently "ok") with ProbeOK=false, ProbeError set, and a
// hint naming the failure.
func TestRunDoctor_ProbeEmbedFails_Degraded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]string{{"name": "bge-m3"}},
			})
		case "/api/embeddings":
			http.Error(w, "model crashed", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dir := doctorTestStore(t)
	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	if err := st.SetSetting(context.Background(), store.SettingOllamaURL, srv.URL); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	_ = st.Close()

	var sb strings.Builder
	err = runDoctorWithLauncher([]string{"--data-dir=" + dir, "--json"}, &sb, fakeHome, noopLauncher)
	if err == nil {
		t.Fatal("expected a degraded error when the probe embed call fails")
	}

	var rep doctorReport
	if jerr := json.Unmarshal([]byte(sb.String()), &rep); jerr != nil {
		t.Fatalf("decode report: %v\nraw: %s", jerr, sb.String())
	}
	if rep.Verdict != "degraded" {
		t.Errorf("verdict = %q, want degraded (probe failed): %+v", rep.Verdict, rep)
	}
	if rep.ProbeOK {
		t.Error("probe_ok should be false when /api/embeddings errors")
	}
	if rep.ProbeError == "" {
		t.Error("probe_error should be set when /api/embeddings errors")
	}
	found := false
	for _, h := range rep.Hints {
		if strings.Contains(h, "ollama embed probe failed") && strings.Contains(h, "ollama run bge-m3") {
			found = true
		}
	}
	if !found {
		t.Errorf("hints = %v, want a probe-failure hint mentioning 'ollama run bge-m3'", rep.Hints)
	}
}

// TestRunDoctor_ModelCheckError_DoesNotHintOllamaPull verifies that a
// HasModel network error (Ping succeeds, but the model-list call fails) is
// surfaced as ModelCheckError and does NOT produce the "ollama pull <model>"
// hint (that hint asserts the model list was read successfully and the
// model is simply absent — a different situation than "couldn't check").
func TestRunDoctor_ModelCheckError_DoesNotHintOllamaPull(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			// First call: Ping's reachability check succeeds.
			json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{}})
			return
		}
		// Second call: HasModel's own request fails.
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	dir := doctorTestStore(t)
	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	if err := st.SetSetting(context.Background(), store.SettingOllamaURL, srv.URL); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	_ = st.Close()

	var sb strings.Builder
	err = runDoctorWithLauncher([]string{"--data-dir=" + dir, "--json"}, &sb, fakeHome, noopLauncher)
	if err == nil {
		t.Fatal("expected a degraded error")
	}

	var rep doctorReport
	if jerr := json.Unmarshal([]byte(sb.String()), &rep); jerr != nil {
		t.Fatalf("decode report: %v\nraw: %s", jerr, sb.String())
	}
	if rep.ModelCheckError == "" {
		t.Error("model_check_error should be set when HasModel's request fails")
	}
	if rep.ModelPresent {
		t.Error("model_present should be false when the check itself failed")
	}
	for _, h := range rep.Hints {
		if strings.Contains(h, "ollama pull") {
			t.Errorf("hints = %v, must NOT contain an 'ollama pull' hint when the model list could not be read", rep.Hints)
		}
	}
	found := false
	for _, h := range rep.Hints {
		if strings.Contains(h, "model list") {
			found = true
		}
	}
	if !found {
		t.Errorf("hints = %v, want a hint explaining the model list could not be read", rep.Hints)
	}
}

// TestRunDoctor_CoverageError_Surfaced verifies that an EmbeddingCoverage
// error (e.g. the settings/coverage context is already done) is surfaced as
// CoverageError instead of silently discarded.
func TestRunDoctor_CoverageError_Surfaced(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	rep := buildDoctorReport(canceledCtx, st, doctorConfig{wait: time.Second}, noopLauncher)
	if rep.CoverageError == "" {
		t.Error("coverage_error should be set when EmbeddingCoverage fails")
	}
}

// ─── routing ──────────────────────────────────────────────────────────────────

func TestRouteCommand_doctor_routes_correctly(t *testing.T) {
	dir := t.TempDir()
	var sb strings.Builder
	// A brand-new store defaults to search.mode=vector with embeddings
	// disabled, which is legitimately "degraded" (exit 1) — routeCommand
	// still dispatches and prints the report either way; only the routing
	// itself (not the verdict) is under test here.
	_ = routeCommand([]string{"ion-mem", "doctor", "--data-dir=" + dir}, &sb)
	if !strings.Contains(sb.String(), "ion-mem doctor") {
		t.Errorf("output missing doctor header: %q", sb.String())
	}
}
