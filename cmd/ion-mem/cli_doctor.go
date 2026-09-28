// `ion-mem doctor` subcommand — diagnoses whether the configured search.mode
// is actually satisfiable: is the store readable, are embeddings enabled, is
// Ollama reachable, is the configured model present, and (if so) does a
// probe embed call succeed. Reports coverage and prints exact fix commands.
//
// Usage:
//
//	ion-mem doctor [--json] [--autostart] [--wait=5s] [--timeout=8s] [--data-dir=...]
//
// Exit codes: 0 = ok (the configured mode is fully satisfiable), 1 =
// degraded (mode needs vectors but something is missing, so searches will
// fall back to lexical), 2 = down (the data directory/store is unreadable).
// Callers that invoke doctor as a health check (e.g. the SessionStart hook)
// must tolerate non-zero exit codes.
//
// --timeout bounds the whole run via a root context (see doctorBudget);
// per-check timeouts derive from the remaining budget so the command always
// returns before it elapses — this is what lets the SessionStart hook call
// doctor directly instead of wrapping it in an external `timeout` command
// (absent on stock macOS). It only takes effect when explicitly passed;
// standalone runs keep the historical fixed ping=2s/probe=10s timeouts.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/arodriguezp2003/ion-mem/internal/embed"
	"github.com/arodriguezp2003/ion-mem/internal/hybrid"
	"github.com/arodriguezp2003/ion-mem/internal/store"
)

// doctorExitError carries the specific process exit code `ion-mem doctor`
// wants for a non-ok verdict (see main.go's exitCoder handling). The ok
// verdict never produces an error (nil is returned from runDoctor instead).
type doctorExitError struct {
	code int
	msg  string
}

func (e *doctorExitError) Error() string { return e.msg }
func (e *doctorExitError) ExitCode() int { return e.code }

// doctorPingTimeout bounds the Ping and HasModel calls when no explicit
// --timeout budget is given (see doctorBudget).
const doctorPingTimeout = 2 * time.Second

// doctorProbeTimeout bounds the ProbeEmbed call when no explicit --timeout
// budget is given (see doctorBudget).
const doctorProbeTimeout = 10 * time.Second

// explicitProbeCap bounds the ProbeEmbed call when an explicit --timeout
// budget IS given: min(explicitProbeCap, remaining budget).
const explicitProbeCap = 4 * time.Second

// defaultDoctorTimeout is the --timeout flag's default value (shown in
// --help). It only takes effect when --timeout is explicitly passed — see
// doctorConfig.timeoutSet and doctorBudget.
const defaultDoctorTimeout = 8 * time.Second

// doctorConfig collects the parsed flags for the `doctor` subcommand.
type doctorConfig struct {
	dataDir   string
	jsonOut   bool
	autostart bool
	wait      time.Duration
	timeout   time.Duration
	// timeoutSet is true only when --timeout was explicitly passed on the
	// command line (fs.Visit, mirroring backfillConfig.verbose in
	// cli_backfill.go). Standalone runs that never pass --timeout keep the
	// historical fixed ping=2s/probe=10s timeouts with no overall deadline
	// (see doctorBudget) — only an explicit --timeout (as the SessionStart
	// hook always passes) activates the derived-from-remaining-budget caps.
	timeoutSet bool
}

func parseDoctorFlags(args []string, homeDir func() (string, error)) (doctorConfig, error) {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	dataDir := fs.String("data-dir", defaultDataDir(homeDir), "Data directory for the SQLite store.")
	jsonOut := fs.Bool("json", false, "Emit the report as JSON instead of aligned text.")
	autostart := fs.Bool("autostart", false, "Attempt to launch Ollama when ollama.autostart=true and it is unreachable.")
	wait := fs.Duration("wait", 5*time.Second, "How long --autostart polls for Ollama to become reachable.")
	timeout := fs.Duration("timeout", defaultDoctorTimeout,
		"Overall wall-clock budget for the whole diagnostic run, applied via a root context. "+
			"Per-check timeouts (ping, HasModel, probe) derive from the remaining budget so the "+
			"command always returns before it elapses. Only takes effect when explicitly passed.")

	if err := parseFlagsWithHelp(fs, args, os.Stdout); err != nil {
		return doctorConfig{}, fmt.Errorf("ion-mem doctor: %w", err)
	}

	cfg := doctorConfig{
		dataDir:   *dataDir,
		jsonOut:   *jsonOut,
		autostart: *autostart,
		wait:      *wait,
		timeout:   *timeout,
	}
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "timeout" {
			cfg.timeoutSet = true
		}
	})
	return cfg, nil
}

// doctorBudget resolves the overall wall-clock deadline for a doctor run
// (when --timeout was explicitly passed) and derives per-check timeouts
// from the remaining time on that budget, so a single doctor invocation
// always returns before the budget elapses without needing an external
// `timeout` wrapper (which is absent on stock macOS).
//
// When --timeout was NOT explicitly passed, ctx carries no deadline and
// pingTimeout/probeTimeout return the historical fixed values (2s/10s) —
// standalone interactive use is unaffected.
type doctorBudget struct {
	ctx      context.Context
	cancel   context.CancelFunc
	explicit bool
}

func newDoctorBudget(parent context.Context, cfg doctorConfig) doctorBudget {
	if !cfg.timeoutSet {
		return doctorBudget{ctx: parent, cancel: func() {}}
	}
	ctx, cancel := context.WithTimeout(parent, cfg.timeout)
	return doctorBudget{ctx: ctx, cancel: cancel, explicit: true}
}

// remaining returns the time left until the budget's deadline (0 if
// already past). Only meaningful when b.explicit is true.
func (b doctorBudget) remaining() time.Duration {
	dl, ok := b.ctx.Deadline()
	if !ok {
		return 0
	}
	r := time.Until(dl)
	if r < 0 {
		return 0
	}
	return r
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

// pingTimeout bounds the Ping and HasModel calls: min(doctorPingTimeout,
// remaining) when a budget is explicit, else the fixed doctorPingTimeout.
func (b doctorBudget) pingTimeout() time.Duration {
	if !b.explicit {
		return doctorPingTimeout
	}
	return minDuration(doctorPingTimeout, b.remaining())
}

// probeTimeout bounds the ProbeEmbed call: min(explicitProbeCap, remaining)
// when a budget is explicit, else the fixed doctorProbeTimeout (SHOULD #3:
// the historical 10s standalone default is kept only when no --timeout is
// given).
func (b doctorBudget) probeTimeout() time.Duration {
	if !b.explicit {
		return doctorProbeTimeout
	}
	return minDuration(explicitProbeCap, b.remaining())
}

// doctorCoverage mirrors store.EmbeddingCoverage's (have, total) pair.
type doctorCoverage struct {
	Have  int `json:"have"`
	Total int `json:"total"`
}

// doctorReport is the full diagnostic result, serialized as-is for --json.
type doctorReport struct {
	SearchMode        string `json:"search_mode"`
	EffectiveMode     string `json:"effective_mode"`
	EmbeddingsEnabled bool   `json:"embeddings_enabled"`
	OllamaURL         string `json:"ollama_url"`
	OllamaReachable   bool   `json:"ollama_reachable"`
	Model             string `json:"model"`
	ModelPresent      bool   `json:"model_present"`
	// ModelCheckError is set when the HasModel call itself failed (e.g. a
	// network error), as opposed to succeeding and simply not finding the
	// model. Distinguishing the two matters for hints: a check failure must
	// not produce the "ollama pull <model>" hint (see resolveDoctorVerdict).
	ModelCheckError string `json:"model_check_error,omitempty"`
	// ProbeOK is true only when ProbeEmbed actually succeeded. It is folded
	// into satisfiability: a reachable Ollama with the model "present" per
	// HasModel's tag list is not enough if the model fails to actually
	// produce an embedding (corrupted install, wrong model type, etc.).
	ProbeOK bool `json:"probe_ok"`
	// ProbeError is the ProbeEmbed error's message when ProbeOK is false and
	// the probe was actually attempted (embeddings enabled, reachable,
	// model present). Empty when the probe was never attempted or succeeded.
	ProbeError     string         `json:"probe_error,omitempty"`
	ProbeDims      int            `json:"probe_dims,omitempty"`
	ProbeLatencyMS float64        `json:"probe_latency_ms,omitempty"`
	Coverage       doctorCoverage `json:"coverage"`
	// CoverageError is set when EmbeddingCoverage itself failed (e.g. a
	// database error). Coverage.Have/Total are then both zero and must not
	// be read as "no observations" — see the coverage hint's guard.
	CoverageError      string   `json:"coverage_error,omitempty"`
	Verdict            string   `json:"verdict"`
	Hints              []string `json:"hints"`
	AutostartAttempted bool     `json:"autostart_attempted"`
	AutostartResult    string   `json:"autostart_result,omitempty"`
}

// ollamaLauncher starts a local Ollama server in the background. Returns an
// error only when the launch attempt itself failed to start (not when
// Ollama subsequently fails to become reachable — the caller polls Ping
// separately for that). Injectable so tests never spawn a real process.
type ollamaLauncher func(dataDir string) error

// launchOllama is the real ollamaLauncher used outside tests. On darwin it
// first tries `open -a Ollama`; if that fails, it falls back to `ollama
// serve` detached (as linux always does), with output redirected to
// <dataDir>/logs/ollama.log so it survives this process's exit.
func launchOllama(dataDir string) error {
	if runtime.GOOS == "darwin" {
		if err := exec.Command("open", "-a", "Ollama").Run(); err == nil {
			return nil
		}
		// Fall through to `ollama serve`.
	}

	logPath := filepath.Join(dataDir, "logs", "ollama.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return fmt.Errorf("cli_doctor.launchOllama: create log dir: %w", err)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("cli_doctor.launchOllama: open log: %w", err)
	}
	defer logFile.Close()

	cmd := exec.Command("ollama", "serve")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	// Setsid detaches the child into its own session so it survives this
	// process exiting (ion-mem doctor is a one-shot CLI invocation).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("cli_doctor.launchOllama: start ollama serve: %w", err)
	}
	return nil
}

// runDoctor is the real entry point used by routeCommand.
func runDoctor(args []string, out io.Writer) error {
	return runDoctorWithLauncher(args, out, os.UserHomeDir, launchOllama)
}

// runDoctorWithLauncher is the testable core of runDoctor: homeDir and
// launch are injected so tests control the data dir default and never spawn
// a real process.
func runDoctorWithLauncher(args []string, out io.Writer, homeDir func() (string, error), launch ollamaLauncher) error {
	cfg, err := parseDoctorFlags(args, homeDir)
	if err != nil {
		return err
	}
	if out == nil {
		out = os.Stdout
	}

	st, err := store.Open(cfg.dataDir)
	if err != nil {
		rep := doctorReport{Verdict: "down", Hints: []string{fmt.Sprintf("data directory unreadable at %s: %v", cfg.dataDir, err)}}
		writeDoctorReport(out, cfg, rep)
		return &doctorExitError{code: 2, msg: fmt.Sprintf("store unreadable: %v", err)}
	}
	defer st.Close()

	rep := buildDoctorReport(context.Background(), st, cfg, launch)
	writeDoctorReport(out, cfg, rep)

	switch rep.Verdict {
	case "degraded":
		reason := "search will fall back to lexical"
		if len(rep.Hints) > 0 {
			reason = rep.Hints[0]
		}
		return &doctorExitError{code: 1, msg: fmt.Sprintf("search degraded to lexical: %s", reason)}
	case "down":
		return &doctorExitError{code: 2, msg: "store unreadable"}
	}
	return nil
}

// buildDoctorReport runs every diagnostic check and assembles the report.
// It derives an internal doctorBudget from cfg (see doctorBudget) so all
// network calls stay bounded without relying on an external `timeout`
// wrapper.
func buildDoctorReport(parentCtx context.Context, st *store.Store, cfg doctorConfig, launch ollamaLauncher) doctorReport {
	budget := newDoctorBudget(parentCtx, cfg)
	defer budget.cancel()
	ctx := budget.ctx

	var rep doctorReport
	rep.SearchMode = st.SettingOrDefault(ctx, store.SettingSearchMode, store.DefaultSearchMode)
	rep.EmbeddingsEnabled = st.SettingOrDefault(ctx, store.SettingEmbeddingsEnabled, "false") == "true"
	rep.OllamaURL = st.SettingOrDefault(ctx, store.SettingOllamaURL, "http://localhost:11434")
	rep.Model = st.SettingOrDefault(ctx, store.SettingEmbeddingsModel, store.DefaultEmbeddingsModel)

	have, total, covErr := st.EmbeddingCoverage(ctx, "", rep.Model)
	rep.Coverage = doctorCoverage{Have: have, Total: total}
	if covErr != nil {
		rep.CoverageError = covErr.Error()
	}

	needsVectors := rep.SearchMode != string(hybrid.ModeLexical)

	if needsVectors {
		client := embed.DefaultClient(rep.OllamaURL)

		rep.OllamaReachable = pingOllama(ctx, client, budget.pingTimeout())

		autostartEnabled := st.SettingOrDefault(ctx, store.SettingOllamaAutostart, store.DefaultOllamaAutostart) == "true"
		if cfg.autostart && autostartEnabled && !rep.OllamaReachable {
			rep.AutostartAttempted = true
			rep.AutostartResult = attemptAutostart(ctx, client, cfg.dataDir, cfg.wait, budget.pingTimeout(), launch)
			rep.OllamaReachable = pingOllama(ctx, client, budget.pingTimeout())
		}

		if rep.OllamaReachable {
			hasCtx, cancel := context.WithTimeout(ctx, budget.pingTimeout())
			present, hasErr := client.HasModel(hasCtx, rep.Model)
			cancel()
			rep.ModelPresent = present
			if hasErr != nil {
				rep.ModelCheckError = hasErr.Error()
			}

			if present {
				probeCtx, cancel := context.WithTimeout(ctx, budget.probeTimeout())
				dims, latency, probeErr := client.ProbeEmbed(probeCtx, rep.Model)
				cancel()
				if probeErr != nil {
					rep.ProbeError = probeErr.Error()
				} else {
					rep.ProbeOK = true
					rep.ProbeDims = dims
					rep.ProbeLatencyMS = float64(latency) / float64(time.Millisecond)
				}
			}
		}
	}

	rep.EffectiveMode, rep.Verdict, rep.Hints = resolveDoctorVerdict(rep, needsVectors)
	return rep
}

// pingOllama runs a Ping bounded by timeout and reports reachability.
func pingOllama(ctx context.Context, client *embed.Client, timeout time.Duration) bool {
	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return client.Ping(pingCtx) == nil
}

// attemptAutostart runs launch, then polls Ping every 500ms up to wait —
// but returns early if ctx's own deadline (the overall doctor budget, when
// --timeout was explicitly passed) arrives first, so autostart can never by
// itself make a doctor run overrun its budget.
func attemptAutostart(ctx context.Context, client *embed.Client, dataDir string, wait, pingTimeout time.Duration, launch ollamaLauncher) string {
	if err := launch(dataDir); err != nil {
		return "failed to launch: " + err.Error()
	}

	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "budget exhausted while waiting for Ollama to become reachable"
		case <-time.After(500 * time.Millisecond):
		}
		if pingOllama(ctx, client, pingTimeout) {
			return "reachable"
		}
	}
	return "still unreachable after " + wait.String()
}

// resolveDoctorVerdict computes the effective mode, verdict, and ordered
// fix-command hints from a partially-built report.
func resolveDoctorVerdict(rep doctorReport, needsVectors bool) (effectiveMode, verdict string, hints []string) {
	if !needsVectors {
		return string(hybrid.ModeLexical), "ok", nil
	}

	if !rep.EmbeddingsEnabled {
		hints = append(hints, "ion-mem config set embeddings.enabled true")
	}
	if rep.EmbeddingsEnabled && !rep.OllamaReachable {
		if runtime.GOOS == "darwin" {
			hints = append(hints, "open -a Ollama")
		} else {
			hints = append(hints, "ollama serve")
		}
	}
	if rep.EmbeddingsEnabled && rep.OllamaReachable && !rep.ModelPresent {
		if rep.ModelCheckError != "" {
			// The model list itself could not be read (e.g. a network
			// error) — this is NOT "the model is absent", so never suggest
			// `ollama pull`, which would be a misleading fix command here.
			hints = append(hints, fmt.Sprintf("could not read Ollama's model list: %s", rep.ModelCheckError))
		} else {
			hints = append(hints, fmt.Sprintf("ollama pull %s", rep.Model))
		}
	}
	if rep.EmbeddingsEnabled && rep.OllamaReachable && rep.ModelPresent && !rep.ProbeOK {
		hints = append(hints, fmt.Sprintf("ollama embed probe failed: %s — check the model with `ollama run %s`", rep.ProbeError, rep.Model))
	}
	if rep.CoverageError != "" {
		hints = append(hints, fmt.Sprintf("could not read embedding coverage: %s", rep.CoverageError))
	} else if rep.Coverage.Total > 0 && rep.Coverage.Have < rep.Coverage.Total {
		hints = append(hints, "ion-mem backfill-embeddings")
	}

	satisfiable := rep.EmbeddingsEnabled && rep.OllamaReachable && rep.ModelPresent && rep.ProbeOK
	if satisfiable {
		return rep.SearchMode, "ok", hints
	}
	return string(hybrid.ModeLexical), "degraded", hints
}

// writeDoctorReport writes rep to out as JSON or aligned text per cfg.jsonOut.
func writeDoctorReport(out io.Writer, cfg doctorConfig, rep doctorReport) {
	if cfg.jsonOut {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rep)
		return
	}
	writeDoctorText(out, rep)
}

func writeDoctorText(out io.Writer, rep doctorReport) {
	fmt.Fprintln(out, "ion-mem doctor")
	fmt.Fprintf(out, "  search_mode:      %s\n", rep.SearchMode)
	fmt.Fprintf(out, "  effective_mode:   %s\n", rep.EffectiveMode)
	fmt.Fprintf(out, "  embeddings:       %v\n", rep.EmbeddingsEnabled)
	if rep.SearchMode != string(hybrid.ModeLexical) {
		fmt.Fprintf(out, "  ollama_url:       %s\n", rep.OllamaURL)
		fmt.Fprintf(out, "  ollama_reachable: %v\n", rep.OllamaReachable)
		fmt.Fprintf(out, "  model:            %s (present=%v)\n", rep.Model, rep.ModelPresent)
		if rep.ProbeDims > 0 {
			fmt.Fprintf(out, "  probe:            %d dims, %.1fms\n", rep.ProbeDims, rep.ProbeLatencyMS)
		}
	}
	fmt.Fprintf(out, "  coverage:         %d/%d embedded\n", rep.Coverage.Have, rep.Coverage.Total)
	if rep.AutostartAttempted {
		fmt.Fprintf(out, "  autostart:        %s\n", rep.AutostartResult)
	}
	fmt.Fprintf(out, "  verdict:          %s\n", rep.Verdict)
	for _, h := range rep.Hints {
		fmt.Fprintf(out, "  hint: %s\n", h)
	}
}

// ollamaHealth is a minimal Ollama reachability/model-presence check shared
// by `ion-mem doctor` and the extra line `ion-mem status` prints when
// embeddings are enabled (see runStatus in cli_status.go).
func ollamaHealth(ctx context.Context, url, model string) (reachable, modelPresent bool) {
	client := embed.DefaultClient(url)
	if !pingOllama(ctx, client, doctorPingTimeout) {
		return false, false
	}
	hasCtx, cancel := context.WithTimeout(ctx, doctorPingTimeout)
	defer cancel()
	present, _ := client.HasModel(hasCtx, model)
	return true, present
}
