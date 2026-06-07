# Tasks: Project Detection (5-Case Algorithm)

## Part 1: Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated production lines | ~450 |
| Estimated test lines | ~600 |
| Total | ~1050 |
| 400-line production budget risk | Low (production alone fits within 400) |
| 400-line total budget risk | High (total ~1050 exceeds budget) |
| Chained PRs recommended | **No — single PR with `size:exception`** |
| Suggested chain | `single-pr` |
| Delivery strategy | `ask-on-risk` |
| Decision needed before apply | Yes — confirm `size:exception` before sdd-apply starts |

**Rationale for single PR:** `internal/project` is a leaf package with no existing consumers. Splitting into two chained PRs (helpers slice / DetectFull slice) is awkward because the helpers have no meaningful integration entry point until `DetectFull` wires them; reviewers would need to hold both PRs in their head anyway. The 13-step TDD apply order guarantees each commit is small and independently reviewable within the single PR. The ~600 test lines are inherently coupled to the ~450 production lines — keeping them together tells the complete story per work-unit-commits conventions.

---

## Part 2: Suggested Work Units

| Work Unit | Title | Scope | Est. Production LOC | Est. Test LOC | Files Touched |
|-----------|-------|-------|---------------------|---------------|---------------|
| WU-1 (single PR) | `feat(project): pure-function project detection with 5-case algorithm` | All 13 apply steps: helpers, algorithm, public surface, doc | ~450 | ~600 | `errors.go`, `noise.go`, `git.go`, `config.go`, `detect.go`, `doc.go`, `detect_test.go`, `git_test.go`, `config_test.go`, `helpers_test.go` |

> `size:exception` required. Awaiting orchestrator confirmation before sdd-apply.

---

## Part 3: Task Checklist (TDD-Ordered)

Each `[TDD-RED]` step must be committed before its matching `[TDD-GREEN]`. The repo must build and tests must fail (compile or runtime) at each RED step before writing production code.

### Phase 1 — Non-Behavioral Setup

- [x] **T-01 `[PREP]`** Create `internal/project/errors.go`
  - Declare `ErrAmbiguousProject = errors.New("project: ambiguous — multiple git repositories found")` (exported sentinel).
  - Declare package-private `wrap(op string, err error) error` returning `fmt.Errorf("project: %s: %w", op, err)`.
  - Add godoc on `ErrAmbiguousProject`.
  - Satisfies: R-API-04, R-CC-04.

- [x] **T-02 `[PREP]`** Create `internal/project/noise.go`
  - Declare `var noiseDirs = map[string]struct{}{}` containing exactly 14 entries: `node_modules`, `vendor`, `.venv`, `venv`, `target`, `dist`, `build`, `.idea`, `.vscode`, `.git`, `bin`, `out`, `cache`, `tmp`.
  - No exported symbols.
  - Satisfies: R-NOISE-01, R-NOISE-02 (14 entries — `.git` included per design §3).

### Phase 2 — `parseRemoteName` (pure function, easiest red→green)

- [x] **T-03 `[TDD-RED]`** Write failing test for `parseRemoteName` in `git_test.go` (package `project`)
  - Table-driven via `t.Run(tt.name, ...)`.
  - Cases must cover: SSH `git@host:org/name.git` → `name` (R-PARSE-01), HTTPS + `.git` suffix (R-PARSE-02), HTTPS no suffix (R-PARSE-04, R-PARSE-05 negative), `ssh://` scheme (R-PARSE-03), URL with query string → strip `?ref=main` (R-PARSE-06 adjacent), malformed (no `:` or `/`) → `""` (R-PARSE-07), leading/trailing whitespace → trim (R-PARSE-06), empty string → `""`.
  - File does not compile until `git.go` exists with the signature. Confirm red.
  - Satisfies: R-PARSE-01 through R-PARSE-07.

- [x] **T-04 `[TDD-GREEN]`** Create `internal/project/git.go`, implement `parseRemoteName(url string) string`
  - Logic: trim whitespace → strip after `?` → strip trailing `.git` → split on `/` and `:` → take last non-empty segment.
  - All T-03 cases pass. No other production code in this file yet.
  - Satisfies: R-PARSE-01 through R-PARSE-07.

### Phase 3 — `normalize` (pure function)

- [x] **T-05 `[TDD-RED]`** Write failing test for `normalize` in `git_test.go` or a new `detect_test.go` section (package `project`)
  - Cases: `"  MyProject  "` → `"myproject"`, `"IONIX"` → `"ionix"`, `""` → `""`, `"  "` (only whitespace) → `""`.
  - Function does not exist yet — confirm red.
  - Satisfies: R-API-08, locked decision #8.

- [x] **T-06 `[TDD-GREEN]`** Implement `normalize(name string) string` in `detect.go` (or `normalize.go`)
  - Body: `return strings.ToLower(strings.TrimSpace(name))`.
  - All T-05 cases pass.
  - Satisfies: R-API-08, locked decision #8.

### Phase 4 — `gitRoot` (IO helper)

- [x] **T-07 `[TDD-RED]`** Write failing test for `gitRoot` in `git_test.go` (package `project`)
  - Use `t.TempDir()` + `initRepo` fixture builder (declared in `helpers_test.go`, created in T-25).
  - Cases (skip in `-short` where they shell to git):
    - `cwd` is a git repo root → `(root, true, nil)`, root == cwd (after symlink resolution).
    - `cwd` is a subdir of a git repo → `(root, true, nil)`, root == repo root.
    - `cwd` is NOT in a git repo → `(_, false, nil)`.
    - Symlink `cwd` pointing to a git repo dir → resolves correctly (macOS `/tmp` → `/private/tmp` covered by `mustAbs`).
  - Function signature missing in git.go → confirm red.
  - Satisfies: R-ALGO-01 (git probe), R-API-06 (symlink handling), R-CC-06 (2s timeout).

- [x] **T-08 `[TDD-GREEN]`** Implement `gitRoot(cwd string) (root string, found bool, err error)` in `git.go`
  - Shell: `git -C <cwd> rev-parse --show-toplevel` with `context.WithTimeout(2s)`.
  - Exit 128 + empty stdout → `("", false, nil)`.
  - `exec.LookPath("git")` failure → `("", false, wrap("git binary not found in PATH", err))`.
  - Trims trailing whitespace from stdout.
  - All T-07 cases pass.
  - Satisfies: R-ALGO-01, R-CC-06.

### Phase 5 — `gitRemoteOrigin` (IO helper)

- [x] **T-09 `[TDD-RED]`** Write failing test for `gitRemoteOrigin` in `git_test.go` (package `project`)
  - Cases (skip in `-short`):
    - Repo with remote set → `(url, true, nil)`.
    - Repo with no remote → `("", false, nil)`.
    - Malformed config (exit non-zero but not "no remote" — simulate via bad git state or mock) → propagated error.
  - Use `initRepo` + `addRemote` fixtures.
  - Satisfies: R-ALGO-05.

- [x] **T-10 `[TDD-GREEN]`** Implement `gitRemoteOrigin(repoRoot string) (url string, found bool, err error)` in `git.go`
  - Shell: `git -C <repoRoot> remote get-url origin` with 2s timeout.
  - Exit 128 + empty stdout ("no such remote") → `("", false, nil)`.
  - Trims result.
  - All T-09 cases pass.
  - Satisfies: R-ALGO-05, R-CC-06.

### Phase 6 — `readConfig` (IO helper, walk-up)

- [x] **T-11 `[TDD-RED]`** Write failing test for `readConfig` in `config_test.go` (package `project`)
  - Cases:
    - Config at `cwd` (nearest wins — locked decision #7): returns `(cfg, true, nil)`.
    - Config at `repoRoot` but not at `cwd` (walks up, finds root): returns `(cfg, true, nil)`.
    - Config at parent above `repoRoot` (boundary): NOT found — returns `("", false, nil)`.
    - Config at `cwd == repoRoot` (no subdirs): checks only that directory.
    - Malformed JSON at nearest: returns `(zero, false, err)` — error propagates, does NOT silently skip (per design §7 — we treat malformed as error since file clearly exists as user intent).
    - Empty `project` field after trim: returns `(zero, false, nil)` — falls through.
    - No config anywhere: returns `(zero, false, nil)`.
  - Use `t.TempDir()` + `writeConfig` fixture builder.
  - Satisfies: R-ALGO-02, R-ALGO-03, R-ALGO-04, locked decisions #6, #7.

- [x] **T-12 `[TDD-GREEN]`** Create `internal/project/config.go`, implement `readConfig(cwd, repoRoot string) (cfg configFile, found bool, err error)`
  - Define `configFile struct { Project string `json:"project"` }`.
  - Walk-up loop: start at `cwd`, check `.ion-mem/config.json`, if not found move to `filepath.Dir(current)`, stop when `current == repoRoot` (check root too) or when `current` would go above `repoRoot`.
  - On found: unmarshal JSON; if error → return wrapped error; if `strings.TrimSpace(cfg.Project) == ""` → continue walking; else return `(cfg, true, nil)`.
  - All T-11 cases pass.
  - Satisfies: R-ALGO-02, R-ALGO-03, R-ALGO-04, locked decision #7.

### Phase 7 — `listGitChildren` (IO helper)

- [x] **T-13 `[TDD-RED]`** Write failing test for `listGitChildren` in `detect_test.go` or a dedicated file (package `project`)
  - Cases:
    - Directory with one git-bearing child → returns `[{Name: "child", Path: <abs>}]` (sorted).
    - Directory with two git-bearing children → returns sorted slice of two.
    - Noise dir with `.git` present → excluded from result (R-NOISE-01, R-NOISE-03).
    - Hidden dir (`.hidden`) with `.git` → excluded (R-ALGO-11).
    - No git-bearing children → empty slice.
    - Scan timeout (200 ms wall-clock): on timeout, returns whatever was collected and falls through — simulated via large dir in integration context or skipped in `-short`.
  - Use `t.TempDir()` + `initRepo` for each child.
  - Satisfies: R-ALGO-07, R-ALGO-08, R-ALGO-10, R-ALGO-11, R-NOISE-01, R-NOISE-02, R-NOISE-03.

- [x] **T-14 `[TDD-GREEN]`** Implement `listGitChildren(cwd string, noise map[string]struct{}) ([]childRepo, error)` in `detect.go`
  - Define `childRepo struct { Name, Path string }`.
  - `os.ReadDir(cwd)` → iterate; skip non-dirs, names in `noise`, names starting with `.`.
  - For each candidate: `os.Stat(filepath.Join(entry.Path, ".git"))` — success → append.
  - Sort result by Name before returning.
  - Enforce 200 ms timeout using a context-cancelled early return or a deadline on the loop (20-entry cap via counter).
  - All T-13 cases pass.
  - Satisfies: R-ALGO-07, R-ALGO-08, R-ALGO-10, R-ALGO-11, R-NOISE-01, R-NOISE-02, R-NOISE-03.

### Phase 8 — `DetectFull` (orchestration, built case-by-case)

> Before T-15, create `internal/project/helpers_test.go` with fixture builders used across T-07/09/11/13:
> `initRepo`, `addRemote`, `writeConfig`, `chdir`, `mustAbs`. These must exist for T-15+ RED steps to compile.

- [x] **T-15 `[PREP]`** Create `internal/project/helpers_test.go` (package `project_test`)
  - `initRepo(t, dir)` — `git init` + minimal commit so git operations work.
  - `addRemote(t, repoDir, url)` — `git -C <dir> remote add origin <url>`.
  - `writeConfig(t, dir, project)` — writes `<dir>/.ion-mem/config.json` with `{"project":"<project>"}`.
  - `chdir(t, dir)` — wraps `t.Chdir(dir)` (Go 1.24+).
  - `mustAbs(t, p)` — `filepath.Abs` + `filepath.EvalSymlinks`, `t.Fatal` on error.
  - Note: `initRepo` called before white-box `git_test.go` tests too — move fixture to shared `_test` package or expose from `helpers_test.go` as appropriate.
  - Satisfies: test infrastructure for all scenario tests.

- [x] **T-16 `[TDD-RED]`** Write failing test for `DetectFull` case 5 (`dir_basename`) in `detect_test.go` (package `project_test`)
  - Scenarios: DIR-BASENAME-01 (plain non-git dir → basename), DIR-BASENAME-02 (root `/` → normalize `filepath.Base("/")`), ERR-01 (relative path → error containing "absolute"), ERR-02 (non-existent path → error), ERR-03 (file not dir → error), ERR-04 (symlink → resolved, no error).
  - Also assert return struct shape: `Project`, `Source`, `Path`, `Warning`, `AvailableProjects` fields present.
  - `DetectFull` does not exist yet → confirm red.
  - Satisfies: R-API-01, R-API-02, R-API-03, R-API-05, R-API-06, R-API-07, R-ALGO-09.

- [x] **T-17 `[TDD-GREEN]`** Create `internal/project/detect.go`, implement `DetectFull` skeleton + preconditions + case 5
  - Export `DetectionResult struct` per R-API-03.
  - Precondition steps 1–3 (filepath.Abs guard, EvalSymlinks, Stat/IsDir).
  - Git probe stub: call `gitRoot` (already implemented).
  - For non-git, non-children path: return `{Project: normalize(filepath.Base(cwd)), Source: "dir_basename", Path: cwd}`.
  - All T-16 cases pass.
  - Satisfies: R-API-02, R-API-03, R-API-05, R-API-06, R-API-07, R-ALGO-09, R-API-08.

- [x] **T-18 `[TDD-RED]`** Write failing test for `DetectFull` case 3 (`git_root`) in `detect_test.go`
  - Scenarios: GIT-ROOT-01 (cwd is repo root), GIT-ROOT-02 (cwd is nested subdir).
  - Assert `Source == "git_root"`, `Project == normalize(filepath.Base(repoRoot))`, `Path == repoRoot`.
  - Use `initRepo` fixture (no remote, no config).
  - Satisfies: R-ALGO-06, R-API-08.

- [x] **T-19 `[TDD-GREEN]`** Wire case 3 (`git_root`) in `DetectFull`
  - After git probe: if `isRepo` and config + remote both fall through, return `{Project: normalize(filepath.Base(repoRoot)), Source: "git_root", Path: repoRoot}`.
  - All T-18 cases pass.
  - Satisfies: R-ALGO-06, R-API-08.

- [x] **T-20 `[TDD-RED]`** Write failing test for `DetectFull` case 2 (`git_remote`) in `detect_test.go`
  - Scenarios: GIT-REMOTE-01 (SSH URL), GIT-REMOTE-02 (HTTPS + `.git`), GIT-REMOTE-03 (HTTPS no `.git`), GIT-REMOTE-04 (malformed URL → falls through to `git_root`), GIT-REMOTE-05 (`ssh://` scheme).
  - Assert `Source == "git_remote"`, `Project` matches normalized parsed name.
  - Use `initRepo` + `addRemote`.
  - Satisfies: R-ALGO-05, R-PARSE-01 through R-PARSE-07, R-API-08.

- [x] **T-21 `[TDD-GREEN]`** Wire case 2 (`git_remote`) in `DetectFull`
  - After config falls through: call `gitRemoteOrigin`, then `parseRemoteName`, then `normalize` — if non-empty return; else fall to case 3.
  - All T-20 cases pass.
  - Satisfies: R-ALGO-05, R-API-08.

- [x] **T-22 `[TDD-RED]`** Write failing test for `DetectFull` case 1 (`config`) in `detect_test.go`
  - Scenarios: CONFIG-01, CONFIG-02 (malformed falls through), CONFIG-03 (empty field falls through), CONFIG-04 (outside-repo boundary ignored), CONFIG-05 (config wins over remote), CONFIG-06 (config in subdir of repo — walk-up nearest wins).
  - Assert `Source == "config"`, `Project == normalize(cfg.project)`, `Path == dir-that-held-config`.
  - Use `initRepo` + `writeConfig` + optional `addRemote`.
  - Satisfies: R-ALGO-02, R-ALGO-03, R-ALGO-04, R-ALGO-05 (priority), locked decisions #6, #7, #8.

- [x] **T-23 `[TDD-GREEN]`** Wire case 1 (`config`) in `DetectFull`
  - Call `readConfig(cwd, repoRoot)`; if `found` and `normalize(cfg.Project) != ""` → return result; else fall through.
  - Note: malformed config returns an error from `readConfig` — design §7 says propagate as wrapped error (intentional divergence from engram; call out inline comment).
  - All T-22 cases pass.
  - Satisfies: R-ALGO-02, R-ALGO-03, R-ALGO-04, locked decisions #6, #7, #8.

- [x] **T-24 `[TDD-RED]`** Write failing test for `DetectFull` case 4 (`git_child`) in `detect_test.go`
  - Scenarios: GIT-CHILD-01 (single child → auto-promoted, Warning set), GIT-CHILD-02 (two children → `ErrAmbiguousProject`, sorted `AvailableProjects`), GIT-CHILD-03 (noise dir filtered), GIT-CHILD-04 (all noise → falls to `dir_basename`), GIT-CHILD-05 (hidden dir filtered).
  - Assert `Source == "git_child"` for single-child; `Source == "ambiguous"` for multi-child.
  - Assert `Warning` starts with `"auto-promoted child repository: "` for single-child (spec §1.5).
  - Assert `AvailableProjects` is sorted for multi-child.
  - Satisfies: R-ALGO-07, R-ALGO-08, R-ALGO-10, R-ALGO-11, R-NOISE-01, R-NOISE-03.

- [x] **T-25 `[TDD-GREEN]`** Wire case 4 (`git_child`) in `DetectFull`
  - Call `listGitChildren(cwd, noiseDirs)`.
  - `len == 1`: call `DetectFull(child.Path)` recursively; override `Source = "git_child"`, set `Warning`.
  - `len >= 2`: return `{Project: "", Source: "ambiguous", Path: cwd, AvailableProjects: sorted names}`, `ErrAmbiguousProject`.
  - `len == 0`: fall to case 5 (`dir_basename`).
  - All T-24 cases pass.
  - Satisfies: R-ALGO-07, R-ALGO-08, R-ALGO-10, R-ALGO-11, R-NOISE-01, R-NOISE-03.

### Phase 9 — `Detect()` wrapper

- [x] **T-26 `[TDD-RED]`** Write failing test for `Detect()` in `detect_test.go` (package `project_test`)
  - Scenarios: DETECT-01 (happy path → `(project, nil)`), DETECT-02 (ambiguous → `("", ErrAmbiguousProject)`), plus one helper-error case (git error → fails open to `dir_basename` basename, nil error).
  - `Detect` does not exist yet → confirm red.
  - Satisfies: R-API-01, DETECT-01, DETECT-02.

- [x] **T-27 `[TDD-GREEN]`** Implement `Detect(cwd string) (string, error)` in `detect.go`
  - Thin wrapper: call `DetectFull(cwd)`.
  - On `ErrAmbiguousProject` → return `("", ErrAmbiguousProject)`.
  - On any other error → return `(filepath.Base(cwd), nil)` (fail open to basename — design §7).
  - On success → return `(result.Project, nil)`.
  - Add godoc.
  - All T-26 cases pass.
  - Satisfies: R-API-01, R-CC-04, DETECT-01, DETECT-02.

### Phase 10 — Refactor and Verification

- [x] **T-28 `[TDD-REFACTOR]`** Review, extract, polish
  - Scan all production files for duplicated patterns (e.g., repeated `filepath.EvalSymlinks` logic).
  - Ensure every exported symbol has a godoc comment (R-CC-04).
  - Run `gofmt -l .` — must produce no output.
  - Run `go vet ./...` — must produce no output.
  - Re-run `go test ./internal/project/... -cover` — coverage must be ≥ 80%.
  - Tests remain green throughout.
  - Satisfies: R-CC-03, R-CC-04, R-CC-05.

- [x] **T-29 `[PREP]`** Update `internal/project/doc.go`
  - Replace scaffold placeholder comment with: `// Package project resolves a project name from a filesystem path. // It applies a deterministic 5-case algorithm (config → git_remote → git_root → // git_child → dir_basename) and always returns a usable result. // The package is safe for concurrent use and never mutates the filesystem.`
  - Satisfies: R-CC-04.

### Phase 11 — Verification and Commit

- [x] **T-30 `[VERIFY]`** Full verification pass
  - `go build ./...` → exit 0.
  - `go test ./internal/project/...` → exit 0.
  - `go test ./internal/project/... -cover` → ≥ 80% statement coverage.
  - `gofmt -l .` → no output.
  - `go vet ./...` → no output.
  - Verify all 5 algorithm cases have ≥ 1 passing scenario test (design §6 coverage table).
  - Verify URL parsing covers ≥ 4 shapes (SSH, HTTPS+.git, HTTPS no-.git, malformed).
  - Verify all 8 locked decisions are visible in implementation (check inline comments where decisions diverge from engram).
  - Satisfies: spec §1.4 acceptance criteria (all items).

- [ ] **T-31 `[COMMIT]`** Work-unit commit
  - Commit message: `feat(project): pure-function project detection with 5-case algorithm`
  - Body: brief note citing spec version and `size:exception` label.
  - All production + test files included in one commit (work-unit-commits convention: tests ship with the behavior they verify).
  - Satisfies: work-unit-commits skill conventions.

- [ ] **T-32 `[PR]`** Pull request (deferred)
  - PR deferred until remote is configured (scaffold-project pattern: commits land on local main).
  - When opened: PR description must list: what to review first (detection algorithm in `detect.go`), what is out of scope (MCP wiring, consolidation), acceptance criteria checklist, and `size:exception` label.

---

## Part 4: Verification Strategy

| Acceptance Criterion (§1.4) | Verification Step | Task |
|-----------------------------|-------------------|------|
| `go build ./...` exits 0 | Run in T-30 | T-30 |
| `go test ./internal/project/...` exits 0 | Run in T-30 | T-30 |
| `gofmt -l .` produces no output | Checked in T-28 (refactor) and confirmed in T-30 | T-28, T-30 |
| `go vet ./...` exits 0 | Checked in T-28 and confirmed in T-30 | T-28, T-30 |
| Coverage ≥ 80% | Enforced by T-28 refactor; confirmed in T-30 | T-28, T-30 |
| All 5 algorithm cases have ≥ 1 passing scenario test | Case 1 → T-22/23; Case 2 → T-20/21; Case 3 → T-18/19; Case 4 → T-24/25; Case 5 → T-16/17 | T-16–T-25 |
| URL parsing covers ≥ 4 shapes | T-03 table covers 7 shapes | T-03, T-04 |
| All 8 locked decisions visibly honored | Inline comments + test assertions; reviewed in T-28 | T-28 |
| No import of internal/* beyond stdlib | Enforced by R-CC-01; checked by `go vet` in T-30 | T-30 |
| `ErrAmbiguousProject` is `errors.New` sentinel | Declared in T-01; asserted via `errors.Is` in T-24 | T-01, T-24 |

---

## Part 5: Risk Acknowledgment

| Risk (Design §10) | Mitigation | Tasks That Address It |
|-------------------|------------|-----------------------|
| `git` binary missing in CI/dev | `exec.LookPath("git")` in `gitRoot` / `gitRemoteOrigin`; returns wrapped error that `Detect()` fails open on | T-08, T-10, T-27 |
| Symlinks (cwd is symlink to different repo — macOS `/tmp` → `/private/tmp`) | `filepath.EvalSymlinks` in preconditions; `mustAbs` helper in tests uses EvalSymlinks too | T-17 (precondition), T-15 (`mustAbs`), T-07 (symlink test case) |
| Windows path quirks | All joins via `filepath.*`; documented as Unix-first; no hardcoded `/` | T-28 (review pass), design §10 note |
| Race: config file deleted mid-detection | Accepted — pure function, no locking, caller retries if needed; documented in design | T-28 (inline comment) |
| Engram divergence on edge cases | `parseRemoteName` table exhaustive (7 shapes); ambiguity returns sorted list; deliberate divergences (malformed config = error, not silent) documented inline | T-03, T-11, T-12, T-23 |
| CONFIG-06 walk-up patch (locked decision #7 added post-parallel) | `readConfig` walks up from `cwd` to `repoRoot`; tested in T-11 (nearest-wins + boundary cases) | T-11, T-12 |
| Normalization post-patch (locked decision #8 added post-parallel) | `normalize()` applied to all success paths; tested in T-05/T-06; asserted in each DetectFull scenario test | T-05, T-06, all T-16–T-25 |

---

## File Map

| File | Created/Modified | Owner Task |
|------|-----------------|------------|
| `internal/project/errors.go` | Created | T-01 |
| `internal/project/noise.go` | Created | T-02 |
| `internal/project/git.go` | Created | T-04 (parseRemoteName), T-08 (gitRoot), T-10 (gitRemoteOrigin) |
| `internal/project/config.go` | Created | T-12 (readConfig, configFile) |
| `internal/project/detect.go` | Created | T-06 (normalize), T-14 (listGitChildren, childRepo), T-17 (DetectFull skeleton + case 5), T-19 (case 3), T-21 (case 2), T-23 (case 1), T-25 (case 4), T-27 (Detect wrapper) |
| `internal/project/doc.go` | Modified | T-29 |
| `internal/project/git_test.go` | Created | T-03, T-05, T-07, T-09 |
| `internal/project/config_test.go` | Created | T-11 |
| `internal/project/detect_test.go` | Created | T-13, T-16, T-18, T-20, T-22, T-24, T-26 |
| `internal/project/helpers_test.go` | Created | T-15 |

---

## Parallelism Notes

All tasks are sequential by design — each `[TDD-RED]` step must commit before its `[TDD-GREEN]` step. RED/GREEN pairs within a phase can logically overlap file editing, but the TDD discipline requires the test failure to be observed before implementation is written.

The only exception: T-01 and T-02 (`[PREP]`) can be done in either order since they have no mutual dependency. They must both be complete before T-03.

T-15 (`helpers_test.go`) can be created alongside T-01/T-02 if the fixture builder signatures are known — they are, from the design §6. Creating it early avoids compilation failures in T-07/T-09/T-11 white-box tests that also use `initRepo`.
