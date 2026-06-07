# Design: Project Detection (5-Case Algorithm)

## 1. Overview

We are building `internal/project`, a pure-function Go package that resolves a project name from a working directory using a deterministic 5-case algorithm (`config` → `git_remote` → `git_root` → `git_child` → `dir_basename`). The package has zero coupling to `internal/store`, no global state, no caching, and no consumer wiring in this change — it is a leaf dependency whose only job is to answer "which project is this cwd?".

A thin, well-shaped surface matters here because every future consumer (MCP server tools, CLI, HTTP, cloud sync) routes memory writes through this single resolver. If `Detect` is impure, hides errors, or returns inconsistent results across callers, every layer above inherits that drift. We mirror upstream engram's algorithm exactly (same priority, same noise set, same warning semantics) so users moving between engram and ion-mem get identical resolution — divergence is a bug, not a feature.

Strict TDD is active. Each architectural element below — the public surface, the per-case algorithm branches, every helper — is shaped to be writable as a failing test first. The package splits across one file per concern (`detect.go`, `git.go`, `config.go`, `noise.go`, `errors.go`) so each red→green cycle touches the smallest possible blast radius.

## 2. Resolved Decisions

| # | Decision | Justification | Upstream ref |
|---|----------|---------------|--------------|
| 1 | Config dir: `.ion-mem/config.json` | Ionix product identity; users migrating run a one-line rename. | engram `detect.go:221` uses `.engram/`; deliberate divergence. |
| 2 | Git access via `os/exec` (`git rev-parse --show-toplevel`, `git remote get-url origin`) | Engram parity, less code than parsing `.git/config`, single canonical source of truth (git itself). | `detect.go:282`, `:381` |
| 3 | Export `DetectionResult` + both `Detect()` and `DetectFull()` | MCP `mem_current_project` needs the full diagnostic now; contract is cheap to commit and trivial to extend additively. | `detect.go:60` (DetectionResult), `:84` (DetectProjectFull), `:344` (DetectProject) |
| 4 | Names: `Detect(cwd) (string, error)` + `DetectFull(cwd) (DetectionResult, error)` | Engram mental model parity; verbs match user expectations from upstream docs. | `detect.go:84`, `:344` (engram uses `DetectProject` / `DetectProjectFull` — we shorten since the package name already says `project`). |
| 5 | `Warning string` field; no typed sentinels in v1 | Only one warning case exists (`git_child` auto-promote); typed sentinels are speculative until a second warning arrives. | `detect.go:68`, `:138` |
| 6 | Config wins over `git_root` on conflict | Explicit user intent overrides inferred identity; engram parity. | `detect.go:93` (config checked first, returns early) |
| 7 | Config lookup walks UP from `cwd` to repo root (nearest wins); does NOT cross repo boundary | Engram parity (caught by spec agent during parallel run; orchestrator's original "anchored at repo root" instruction was wrong). Monorepo-friendly: subproject configs override parent configs. | engram `detect.go` — walk-up-with-boundary |
| 8 | Normalize ALL emitted project names via `strings.TrimSpace + strings.ToLower` regardless of source | Engram parity + required for store consistency (`observations.project` column is case-sensitive). Empty after normalize → fall through to next case. | engram `detect.go` — `normalize()` helper |

## 3. Architecture

### Package layout

```
internal/project/
├── doc.go              (existing; rewritten with real package doc)
├── detect.go           public Detect, DetectFull, DetectionResult
├── git.go              internal: gitRoot, gitRemoteOrigin
├── config.go           internal: readConfig, configFile
├── noise.go            internal: noiseDirs set
├── errors.go           ErrAmbiguousProject + wrap helper
├── detect_test.go      black-box (package project_test); per-case tables
├── git_test.go         white-box (package project); git helper integration
├── config_test.go      white-box; config reader + JSON edge cases
└── helpers_test.go     black-box; fixture builders (initRepo, addRemote, writeConfig, chdir)
```

### File responsibilities (one line each)

- **`detect.go`** — public `Detect`, `DetectFull`, `DetectionResult`; orchestrates the 5-case flow. Imports `git.go`, `config.go`, `noise.go`, `errors.go`.
- **`git.go`** — `gitRoot(cwd) (root string, found bool, err error)` and `gitRemoteOrigin(repoRoot) (url string, found bool, err error)`. Shells via `os/exec.CommandContext` with 2s timeout. The `(value, found, error)` triplet lets callers distinguish "not a git repo" (`found=false, err=nil`) from "git binary failed" (`err != nil`). Also exposes `parseRemoteName(url) string`.
- **`config.go`** — `readConfig(cwd, repoRoot string) (cfg configFile, found bool, err error)` walks UP from `cwd` toward `repoRoot`, returning the **nearest** `.ion-mem/config.json` (engram parity — monorepo-friendly: sub-project configs override parent configs). Walk stops at `repoRoot` (do NOT cross the enclosing git repo boundary). When `cwd == repoRoot`, only that directory is checked. Outside git, only the exact `cwd` is consulted. Reconciled with spec § R-ALGO-04: spec agent caught that "anchored at repo root" diverged from engram; engram-parity walk-up wins.
- **Normalization** (added to align with engram + spec § R-API-08): every project name returned by `DetectFull` (regardless of source) MUST pass through `normalize(name) string` which applies `strings.TrimSpace + strings.ToLower`. Empty result after normalization → fall through to next case. This guarantees store consistency: the `observations.project` column is case-sensitive, so detection MUST emit a canonical form. Helper lives in `detect.go` or a new `normalize.go` (apply decides).
- **`noise.go`** — `var noiseDirs = map[string]struct{}{...}` containing: `node_modules`, `vendor`, `.venv`, `venv`, `target`, `dist`, `build`, `.idea`, `.vscode`, `.git`, `bin`, `out`, `cache`, `tmp`.
- **`errors.go`** — `ErrAmbiguousProject = errors.New(...)` sentinel. Wrap helper `wrap(op string, err error) error` returning `fmt.Errorf("project: %s: %w", op, err)`.
- **`helpers_test.go`** — fixture builders (see §6).

### Dependency rule

`internal/project` depends **only** on the Go stdlib (`os`, `os/exec`, `path/filepath`, `encoding/json`, `errors`, `fmt`, `strings`, `context`, `time`, `sort`). NO imports from `internal/store`, `internal/mcp`, or any other ion-mem package. This is a leaf package and stays one.

## 4. The 5-Case Algorithm (formal)

```
DetectFull(cwd string) (DetectionResult, error):

  // Precondition normalization
  1. cwd, err = filepath.Abs(cwd); if err: return wrap("abs", err)
  2. cwd, err = filepath.EvalSymlinks(cwd); if err: return wrap("evalsymlinks", err)
  3. info, err = os.Stat(cwd); if err or !info.IsDir(): return wrap("stat", err-or-not-a-dir)

  // Git probe
  4. repoRoot, isRepo, err = gitRoot(cwd); if err: return wrap("gitroot", err)

  5. if isRepo:
       // Case 1: config (config wins over git_root — decision #6)
       cfg, found, err = readConfig(repoRoot)
       if err:       return wrap("readconfig", err)
       if found:     return {Project: cfg.Project,         Source: "config",     Path: repoRoot}, nil

       // Case 2: git_remote
       url, found, err = gitRemoteOrigin(repoRoot)
       if err:       return wrap("gitremote", err)
       if found:
         name = parseRemoteName(url)
         if name != "": return {Project: name,             Source: "git_remote", Path: repoRoot}, nil
         // malformed URL → fall through to case 3

       // Case 3: git_root
       return {Project: filepath.Base(repoRoot),           Source: "git_root",   Path: repoRoot}, nil

  // Not a git repo — try git_child
  6. children, err = listGitChildren(cwd, noiseDirs); if err: return wrap("listchildren", err)
  7. if len(children) == 1:
       child = children[0]
       return {Project: filepath.Base(child.Path),
               Source:  "git_child",
               Path:    child.Path,
               Warning: "auto-promoted child repository: " + filepath.Base(child.Path)}, nil
  8. if len(children) >= 2:
       names = sorted basenames of children
       return {Project: "", Source: "", Path: cwd,
               Error: ErrAmbiguousProject,
               AvailableProjects: names}, ErrAmbiguousProject

  // Case 5: dir_basename (always succeeds)
  9. return {Project: filepath.Base(cwd), Source: "dir_basename", Path: cwd}, nil
```

The apply agent translates this directly to Go. The numbering above is the test order spine (§8).

## 5. Helper Functions

### `parseRemoteName(url string) string` (in `git.go`)

| Input | Output |
|-------|--------|
| `git@github.com:org/repo.git` | `repo` |
| `https://github.com/org/repo.git` | `repo` |
| `ssh://user@host.tld/org/repo.git` | `repo` |
| `https://github.com/org/repo` | `repo` |
| `https://host/org/repo?ref=main` | `repo` |
| `  https://x/y/z.git  ` (whitespace) | `z` |
| `` (empty) / malformed | `""` (fail open) |

Implementation rules:
- Trim leading/trailing whitespace.
- Strip everything after the first `?` (query strings).
- Strip trailing `.git` suffix (once).
- Split on `/` and `:` (handles SSH `host:org/repo` form uniformly with HTTPS).
- Take last non-empty segment; if none → return `""`.
- Fail open: malformed URL returns `""`, caller falls through to `git_root`. No error raised.

### `listGitChildren(cwd string, noise map[string]struct{}) ([]childRepo, error)` (in `detect.go`)

```go
type childRepo struct {
    Name string // basename
    Path string // absolute path
}
```

Walks one level deep from `cwd`. Per subdir entry:
1. Skip if not a directory.
2. Skip if name is in `noise` set.
3. Skip if name starts with `.` (hidden — matches engram `detect.go:316`).
4. Check `<subdir>/.git` exists (Stat, ignore type — engram parity at `:326`).
5. If yes, append `childRepo{Name: basename, Path: absolute path}`.

Returns the list **sorted by Name** for deterministic ambiguity ordering. Note: we deliberately do NOT short-circuit at 2 (engram does at `:330`) — keeping the simpler shape gives deterministic `AvailableProjects` and the perf cost on a typical workspace is negligible. If profiling shows otherwise post-MVP, add short-circuit + secondary sort.

Errors: `os.ReadDir` failure propagates. Per-entry `os.Stat` errors are silently ignored (entry treated as not-a-repo) — matches engram behavior.

## 6. Test Strategy

### Test layout

| File | Package | What |
|------|---------|------|
| `detect_test.go` | `project_test` | One table per case (1–5); cross-case priority tests; full-flow integration through public `Detect`/`DetectFull`. |
| `git_test.go` | `project` | `gitRoot`, `gitRemoteOrigin`, `parseRemoteName` (URL shape table) — white-box because we test internals directly. |
| `config_test.go` | `project` | `readConfig` happy path + malformed JSON + missing file + empty `project` field. |
| `helpers_test.go` | `project_test` | Fixture builders shared by `detect_test.go`. |

### Fixture builders (`helpers_test.go`)

- `initRepo(t *testing.T, dir string)` — runs `git init` + `git -c user.email=t@t -c user.name=t commit --allow-empty -m init`. Some git operations require ≥1 commit; we make repos minimally valid.
- `addRemote(t *testing.T, repoDir, url string)` — runs `git -C <dir> remote add origin <url>`.
- `writeConfig(t *testing.T, repoDir, project string)` — writes `<repoDir>/.ion-mem/config.json` with `{"project": "<project>"}`. Creates `.ion-mem/` if needed.
- `chdir(t *testing.T, dir string)` — wraps `t.Chdir(dir)` (Go 1.24+, available in 1.25). Cleanup is automatic.
- `mustAbs(t *testing.T, p string) string` — `filepath.Abs` + `filepath.EvalSymlinks`, fatal on error (macOS `/tmp` is symlinked to `/private/tmp` and breaks naive comparisons).

### Coverage table

| Case | Happy path | Edge cases |
|------|-----------|------------|
| 1 config | config in repo root | malformed JSON; empty `project`; missing file → falls to case 2 |
| 2 git_remote | SSH URL; HTTPS URL; no `.git` suffix | malformed URL → falls to case 3; no remote → falls to case 3 |
| 3 git_root | called from subdir; called from repo root | symlinked path (EvalSymlinks normalization) |
| 4 git_child | exactly one child repo | noise dirs ignored; hidden dirs ignored; 2+ children → `ErrAmbiguousProject` with sorted `AvailableProjects` |
| 5 dir_basename | non-git non-parent dir | empty cwd → wrap error from `os.Stat` |

Cross-case priority tests (e.g., config beats git_remote when both present) live in `detect_test.go` to lock decision #6.

Coverage target ≥ 80% (matches `local-store-mvp` and §10 of project conventions). Verified via `go test ./internal/project/... -cover`.

## 7. Error Handling

| Site | Behavior |
|------|----------|
| `DetectFull` ambiguous git_child | Returns `(DetectionResult{Error: ErrAmbiguousProject, AvailableProjects: [...]}, ErrAmbiguousProject)`. Result fields are populated for caller diagnostics; error is returned so `errors.Is(err, ErrAmbiguousProject)` works. |
| `DetectFull` internal helper failures (git exec error, IO error reading config, stat failure) | Wrapped as `fmt.Errorf("project: <op>: %w", err)` and returned. Result is zero value. |
| `Detect()` wrapper | Returns `(name, nil)` on success. On `ErrAmbiguousProject` returns `("", ErrAmbiguousProject)`. On ANY other wrapped helper error, returns `(filepath.Base(cwd), nil)` — **fails open to dir_basename**. Rationale: `Detect` is the convenience path; callers wanting full diagnostics use `DetectFull`. |
| `readConfig` malformed JSON | Returns `(zero, false, fmt.Errorf("project: parseconfig: %w", err))` — propagates as wrapped error from `DetectFull`. (Engram returns an invalidConfig result; we treat it as an error because the file existed and the user clearly intended it as authoritative — silent fallback hides bugs.) |
| `gitRoot` / `gitRemoteOrigin` non-zero exit | Distinguish "not a repo / no remote" (exit code 128 with empty stdout) from "git binary missing or other fatal" (`exec.LookPath` failure). The former returns `(value, false, nil)`; the latter returns `(zero, false, error)`. |

`ErrAmbiguousProject` is the **only** sentinel exported in v1. Other internal errors are wrapped via `fmt.Errorf` with `%w` so callers can `errors.As` if they ever need to.

## 8. Strict TDD Operational Notes (apply order)

| # | Step | Mode | What |
|---|------|------|------|
| 1 | `errors.go`, `noise.go` | `[PREP]` | No behavior — declare `ErrAmbiguousProject` and `noiseDirs`. |
| 2 | `parseRemoteName` | `[TDD]` | Pure function, easiest red→green. Table-driven from §5. |
| 3 | `gitRoot` | `[TDD]` | Use `initRepo` fixture. Cases: in-repo, not-a-repo, subdir. |
| 4 | `gitRemoteOrigin` | `[TDD]` | Use `initRepo` + `addRemote`. Cases: with remote, no remote. |
| 5 | `readConfig` | `[TDD]` | `t.TempDir` + `writeConfig`. Cases: present, missing, malformed JSON, empty project. |
| 6 | `listGitChildren` | `[TDD]` | Cases: zero children, one child, two children sorted, noise dirs filtered, hidden dirs filtered. |
| 7 | `DetectFull` case 5 (dir_basename) | `[TDD]` | Simplest path — non-git, no children. Locks return shape. |
| 8 | `DetectFull` case 3 (git_root) | `[TDD]` | Add git repo to fixture; assert source/path. |
| 9 | `DetectFull` case 2 (git_remote) | `[TDD]` | Add remote; assert priority over case 3. |
| 10 | `DetectFull` case 1 (config) | `[TDD]` | Write config; assert priority over cases 2 + 3 (locks decision #6). |
| 11 | `DetectFull` case 4 (git_child) | `[TDD]` | Most complex — both single-child and ambiguous branches. |
| 12 | `Detect()` wrapper | `[TDD]` | Happy path delegates to `DetectFull`; ambiguous returns `("", ErrAmbiguousProject)`; helper errors fail open to basename. |
| 13 | Refactor | `[TDD-REFACTOR]` | Extract common patterns, polish names, ensure `gofmt`/`go vet` clean, recheck coverage ≥ 80%. |

Each `[TDD]` row: one (or a few) failing tests committed first (red), minimum implementation to pass (green), refactor with tests still green.

## 9. Open Questions

None. All six proposal questions are resolved in §2. Algorithm flow, helper shapes, error policy, and test layout are all locked.

## 10. Risks

| Risk | Mitigation |
|------|------------|
| `git` binary missing in CI/dev env | `gitRoot` / `gitRemoteOrigin` use `exec.LookPath("git")` check; if absent, return `(zero, false, fmt.Errorf("project: git binary not found in PATH"))` from the helper. `DetectFull` propagates; `Detect()` fails open to `dir_basename`. CI runs `git --version` in setup; documented in spec. |
| Symlinks (cwd is a symlink to a different repo) | `filepath.EvalSymlinks` called in precondition step 2. macOS `/tmp` → `/private/tmp` is the canonical test case; `mustAbs` helper handles it in fixtures. |
| Windows path quirks (case-insensitive, `\` separator) | All path joins use `filepath.*` stdlib; no hardcoded `/`. v1 target is Unix-first; Windows works but is not battle-tested. Documented as a known limitation in spec. |
| Race: `.ion-mem/config.json` deleted mid-detection | Accept the race — `DetectFull` is a pure function, caller retries if needed. No locks, no caching. |
| Engram divergence on edge cases (URL shapes, ambiguous ordering) | `parseRemoteName` table is exhaustive against engram fixtures; ambiguity returns sorted list (deterministic). Any deliberate divergence (e.g., we error on malformed config, engram returns InvalidConfig result) is called out in spec §Decisions. |
