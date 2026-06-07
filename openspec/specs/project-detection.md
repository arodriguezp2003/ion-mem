# Project Detection — Specification

Status: Active
Shipped: 2026-06-07 via SDD change project-detection (single PR, ~470 production + ~1257 test lines, 43 tests, 88.3% coverage).

## 1. Capability: project-detection

### 1.1 Summary

`internal/project` is a pure-function package that resolves a project name from a
filesystem path using a deterministic 5-case algorithm: config → git_remote →
git_root → git_child → dir_basename. The package never blocks on I/O beyond
bounded `git` shellouts (2 s timeout), never mutates the filesystem, and always
returns a usable result because `dir_basename` is the unconditional fallback. All
project names are normalized (lowercased, whitespace trimmed) for consistency with
the store layer.

---

### 1.2 Requirements

#### Public Surface

| ID | Strength | Requirement |
|----|----------|-------------|
| R-API-01 | MUST | Export `Detect(cwd string) (string, error)` returning `(project, nil)` on success or `("", ErrAmbiguousProject)` on ambiguity. |
| R-API-02 | MUST | Export `DetectFull(cwd string) (DetectionResult, error)` returning the full result struct. |
| R-API-03 | MUST | Export `DetectionResult` struct with fields `Project string`, `Source string`, `Path string`, `Warning string`, `AvailableProjects []string`. |
| R-API-04 | MUST | Export `ErrAmbiguousProject` as a sentinel `error` value via `errors.New`. |
| R-API-05 | MUST | Reject relative `cwd` values; return a wrapped error when `cwd` is not absolute. |
| R-API-06 | MUST | Resolve symlinks in `cwd` via `filepath.EvalSymlinks` before running any detection logic. |
| R-API-07 | MUST | Return error when `cwd` does not exist or is not a directory. |
| R-API-08 | MUST | Apply canonical normalization (lowercase + trim whitespace) to all resolved project names. |

#### Algorithm — 5 Cases in Priority Order

| ID | Strength | Requirement |
|----|----------|-------------|
| R-ALGO-01 | MUST | Attempt detection in exactly this order: config → git_remote → git_root → git_child → dir_basename. |
| R-ALGO-02 | MUST | (config) When `cwd` is inside a git repo AND `.ion-mem/config.json` exists at the repo root AND parses with a non-empty `project` field, return `{Project: <config.project>, Source: "config", Path: <repoRoot>}`. |
| R-ALGO-03 | MUST | (config malformed) When `.ion-mem/config.json` exists but JSON is malformed OR `project` is empty, fall through to the next case silently — do NOT return an error. |
| R-ALGO-04 | MUST | (config boundary) Search for `.ion-mem/config.json` only within the enclosing git repo boundary (walk from `cwd` upward to git root, inclusive). Do NOT look past git root. |
| R-ALGO-05 | MUST | (git_remote) When `cwd` is inside a git repo AND `git remote get-url origin` returns a non-empty URL, parse the URL per R-PARSE-* and return `{Project: <parsed>, Source: "git_remote", Path: <repoRoot>}`. |
| R-ALGO-06 | MUST | (git_root) When `cwd` is inside a git repo but git_remote produced no result, return `{Project: filepath.Base(repoRoot) normalized, Source: "git_root", Path: <repoRoot>}`. |
| R-ALGO-07 | MUST | (git_child) When `cwd` is NOT inside a git repo AND exactly one immediate subdir (after noise filtering) contains `.git`, return the detection result for that child with `Warning: "auto-promoted child repository: <childName>"` and `Source: "git_child"`. |
| R-ALGO-08 | MUST | (git_child ambiguity) When 2 or more immediate subdirs (after noise filtering) contain `.git`, return `{Project: "", Source: "ambiguous", Path: cwd, AvailableProjects: <sorted child names>}` with `ErrAmbiguousProject` as the returned error. |
| R-ALGO-09 | MUST | (dir_basename) When no other case applies, return `{Project: filepath.Base(cwd) normalized, Source: "dir_basename", Path: cwd}` with nil error. |
| R-ALGO-10 | MUST | Apply a 200 ms wall-clock timeout and a 20-entry scan cap to the `git_child` directory scan; on timeout fall through to `dir_basename`. |
| R-ALGO-11 | MUST | Skip hidden directories (any name starting with `.`) during `git_child` scanning, in addition to noise-set entries. |

#### URL Parsing

| ID | Strength | Requirement |
|----|----------|-------------|
| R-PARSE-01 | MUST | Handle SSH form `git@host:org/name.git` → name = `name`. |
| R-PARSE-02 | MUST | Handle HTTPS form `https://host/org/name.git` → name = `name`. |
| R-PARSE-03 | MUST | Handle `ssh://user@host/org/name.git` → name = `name`. |
| R-PARSE-04 | MUST | Handle URLs without `.git` suffix → name = last path segment as-is. |
| R-PARSE-05 | MUST | Strip trailing `.git` suffix before extracting the last segment. |
| R-PARSE-06 | MUST | Trim leading/trailing whitespace from the raw URL before parsing. |
| R-PARSE-07 | MUST | Return empty string on malformed input (no `:` or `/` separator found); caller falls through. |

#### Noise Filtering (git_child Only)

| ID | Strength | Requirement |
|----|----------|-------------|
| R-NOISE-01 | MUST | Skip directories whose names appear in the noise set during `git_child` enumeration. |
| R-NOISE-02 | MUST | Noise set MUST contain at minimum: `node_modules`, `vendor`, `.venv`, `venv`, `target`, `dist`, `build`, `.idea`, `.vscode`, `bin`, `out`, `cache`, `tmp`. |
| R-NOISE-03 | MUST | Noise filtering is case-sensitive. |

#### Cross-Cutting

| ID | Strength | Requirement |
|----|----------|-------------|
| R-CC-01 | MUST NOT | Depend on any other `internal/*` package (leaf package: stdlib + `os/exec` only). |
| R-CC-02 | MUST NOT | Mutate the filesystem. |
| R-CC-03 | MUST | Be safe for concurrent use (no shared mutable state at runtime). |
| R-CC-04 | MUST | Provide package-doc comments on all exported functions, types, and the sentinel error. |
| R-CC-05 | MUST | Achieve ≥ 80% statement coverage on `internal/project/...`. |
| R-CC-06 | MUST | Use a 2 s context timeout on every `git` shellout. |

---

### 1.3 Scenarios

#### Config Scenarios

**CONFIG-01 — config wins over remote**

- GIVEN `cwd` is inside a git repo
- AND `.ion-mem/config.json` at repo root contains `{"project":"my-proj"}`
- AND `remote.origin.url` is set to some value
- WHEN `DetectFull(cwd)` is called
- THEN returns `{Project: "my-proj", Source: "config", Path: <repoRoot>, Warning: "", AvailableProjects: nil}` with nil error

**CONFIG-02 — malformed config falls through**

- GIVEN `.ion-mem/config.json` exists at repo root but contains invalid JSON
- WHEN `DetectFull(cwd)` is called
- THEN config case is skipped; detection continues to `git_remote` (or further); no error is returned for the config failure

**CONFIG-03 — empty project field falls through**

- GIVEN `.ion-mem/config.json` exists and parses successfully but `project` is `""`
- WHEN `DetectFull(cwd)` is called
- THEN config case is skipped; detection continues to `git_remote` (or further)

**CONFIG-04 — config outside git repo boundary is ignored**

- GIVEN a git repo at `/tmp/repo` with no `.ion-mem/config.json` inside it
- AND a `.ion-mem/config.json` exists at `/tmp` (parent of repo root)
- WHEN `DetectFull("/tmp/repo/subdir")` is called
- THEN the parent-level config is NOT honored; detection proceeds to `git_remote` or `git_root`

**CONFIG-05 — config honored when both config and remote exist**

- GIVEN `.ion-mem/config.json` with `{"project":"foo"}` at repo root
- AND `remote.origin.url = https://github.com/org/bar.git`
- WHEN `DetectFull(cwd)` is called
- THEN `Project = "foo"`, `Source = "config"`

**CONFIG-06 — config found in subdir of repo (walk-up behavior)**

- GIVEN `cwd = /tmp/repo/pkg/sub` inside a git repo at `/tmp/repo`
- AND `.ion-mem/config.json` with `{"project":"sub-proj"}` exists at `/tmp/repo/pkg`
- WHEN `DetectFull(cwd)` is called
- THEN `Project = "sub-proj"`, `Source = "config"`, `Path = "/tmp/repo/pkg"`

#### git_remote Scenarios

**GIT-REMOTE-01 — SSH URL**

- GIVEN repo with `remote.origin.url = git@github.com:ionix/ion-mem.git`
- WHEN `DetectFull(cwd)` is called
- THEN `Project = "ion-mem"`, `Source = "git_remote"`

**GIT-REMOTE-02 — HTTPS URL with .git suffix**

- GIVEN `remote.origin.url = https://github.com/ionix/ion-mem.git`
- WHEN `DetectFull(cwd)` is called
- THEN `Project = "ion-mem"`, `Source = "git_remote"`

**GIT-REMOTE-03 — HTTPS URL without .git suffix**

- GIVEN `remote.origin.url = https://github.com/ionix/ion-mem`
- WHEN `DetectFull(cwd)` is called
- THEN `Project = "ion-mem"`, `Source = "git_remote"`

**GIT-REMOTE-04 — malformed URL falls through to git_root**

- GIVEN `remote.origin.url = "not-a-url"` (no `:` or `/`)
- WHEN `DetectFull(cwd)` is called
- THEN falls through; `Source = "git_root"`

**GIT-REMOTE-05 — ssh:// scheme**

- GIVEN `remote.origin.url = ssh://git@github.com/ionix/ion-mem.git`
- WHEN `DetectFull(cwd)` is called
- THEN `Project = "ion-mem"`, `Source = "git_remote"`

#### git_root Scenarios

**GIT-ROOT-01 — cwd is repo root**

- GIVEN a git repo at `/tmp/abc/myproj` with no remote and no config
- WHEN `DetectFull("/tmp/abc/myproj")` is called
- THEN `Project = "myproj"`, `Source = "git_root"`, `Path = "/tmp/abc/myproj"`

**GIT-ROOT-02 — cwd is nested inside repo**

- GIVEN a git repo at `/tmp/abc/myproj`
- WHEN `DetectFull("/tmp/abc/myproj/deep/nested/dir")` is called
- THEN `Project = "myproj"`, `Source = "git_root"`, `Path = "/tmp/abc/myproj"`

#### git_child Scenarios

**GIT-CHILD-01 — single git child auto-promoted**

- GIVEN `cwd` is NOT inside a git repo
- AND `cwd` contains exactly one subdir `myproj/` which IS a git repo (has `.git`)
- WHEN `DetectFull(cwd)` is called
- THEN `Project = "myproj"`, `Source = "git_child"`, `Warning` starts with `"auto-promoted"`

**GIT-CHILD-02 — two git children → ambiguous**

- GIVEN `cwd` contains subdirs `a/` and `b/`, both git repos
- WHEN `DetectFull(cwd)` is called
- THEN error == `ErrAmbiguousProject` AND `AvailableProjects = ["a", "b"]` (sorted), `Project = ""`

**GIT-CHILD-03 — noise dir filtered out**

- GIVEN `cwd` contains `node_modules/` (which has `.git`) and `myproj/` (which has `.git`)
- WHEN `DetectFull(cwd)` is called
- THEN `node_modules` is excluded from scan; `Project = "myproj"`, `Source = "git_child"`

**GIT-CHILD-04 — all children are noise → dir_basename fallback**

- GIVEN `cwd` contains only `node_modules/` (which has `.git`); no other subdirs
- WHEN `DetectFull(cwd)` is called
- THEN falls through to `dir_basename`; `Source = "dir_basename"`

**GIT-CHILD-05 — hidden dirs are skipped**

- GIVEN `cwd` contains `.hidden-repo/` (which has `.git`) and `myproj/` (which has `.git`)
- WHEN `DetectFull(cwd)` is called
- THEN `.hidden-repo` is excluded (hidden dir); `Project = "myproj"`, `Source = "git_child"`

#### dir_basename Scenarios

**DIR-BASENAME-01 — plain directory**

- GIVEN `cwd = /tmp/abc/standalone` with no git, no config, no git-bearing children
- WHEN `DetectFull(cwd)` is called
- THEN `Project = "standalone"`, `Source = "dir_basename"`, error = nil

**DIR-BASENAME-02 — root directory**

- GIVEN `cwd = /`
- WHEN `DetectFull("/")` is called
- THEN `Project` is the result of `filepath.Base("/")` normalized (Go returns `"/"`, which normalizes to `"unknown"` or the platform separator basename); `Source = "dir_basename"`, error = nil

#### Detect() Wrapper Scenarios

**DETECT-01 — happy path delegates to DetectFull**

- GIVEN any `DetectFull` happy path that returns a non-empty project
- WHEN `Detect(cwd)` is called
- THEN returns `(project, nil)`

**DETECT-02 — ambiguous returns sentinel error**

- GIVEN the `git_child` ambiguous case
- WHEN `Detect(cwd)` is called
- THEN returns `("", ErrAmbiguousProject)`

#### Error / Guard Scenarios

**ERR-01 — relative path rejected**

- GIVEN `cwd = "./foo"` (relative)
- WHEN `DetectFull(cwd)` is called
- THEN returns error containing `"absolute"` (or equivalent); no detection runs

**ERR-02 — non-existent path rejected**

- GIVEN `cwd` is a path that does not exist on the filesystem
- WHEN `DetectFull(cwd)` is called
- THEN returns an error indicating the path does not exist

**ERR-03 — regular file rejected**

- GIVEN `cwd` points to a regular file (not a directory)
- WHEN `DetectFull(cwd)` is called
- THEN returns an error indicating the path is not a directory

**ERR-04 — symlink resolved transparently**

- GIVEN `cwd` is a symlink pointing to a valid git repo directory
- WHEN `DetectFull(cwd)` is called
- THEN symlink is resolved via `filepath.EvalSymlinks`; detection proceeds against the target; no error is returned for the symlink itself

---

### 1.4 Acceptance Criteria

- [x] `go build ./...` exits 0
- [x] `go test ./internal/project/...` exits 0
- [x] `gofmt -l .` produces no output
- [x] `go vet ./...` exits 0
- [x] Coverage on `internal/project/...` ≥ 80% (actual: 88.3%)
- [x] All 5 algorithm cases have at least one passing scenario test
- [x] URL parsing covers ≥ 4 URL shapes (SSH, HTTPS+`.git`, HTTPS no-`.git`, malformed)
- [x] All 6 locked decisions are visibly honored in the implementation
- [x] No import of any `internal/*` package other than stdlib and `os/exec`
- [x] `ErrAmbiguousProject` is a package-level `errors.New` sentinel, not a constructed error type

---

### 1.5 Behavioral Notes (Engram Parity)

The following behaviors are confirmed from reading `engram-source/internal/project/detect.go`
and MUST be matched by ion-mem. Any intentional divergence must be documented inline.

| Topic | Engram Behavior | Ion-mem Spec Decision |
|-------|-----------------|-----------------------|
| Config field name | `project_name` (JSON key) | `project` (locked decision — intentional divergence) |
| Config dir | `.engram/config.json` | `.ion-mem/config.json` (locked decision) |
| Config walk direction | upward from `cwd` to git root (nearest wins) | Same — walk from `cwd` up to git root |
| `git_child` Source field | `"git_child"` (SourceGitChild) | `"git_child"` (match engram; proposal note resolved here) |
| `git_child` Warning text | `"auto-promoted child repository: <name>"` | Same prefix required |
| Ambiguous Source field | `"ambiguous"` | `"ambiguous"` (match engram) |
| Noise set hidden dirs | All `.` prefix dirs skipped separately | Same — skip hidden dirs AND noise set |
| `git` command | `git -C <dir> rev-parse --show-toplevel` | Same |
| Remote command | `git -C <dir> remote get-url origin` | Same |
| Normalization | `strings.ToLower + TrimSpace` | MUST apply to all resolved names |
| Scan timeout | 200 ms + 20-entry cap | MUST match |
| `git` timeout | 2 s via context | MUST match |

---

### 1.6 Out of Scope

- Name similarity / consolidation (`project-consolidate` change)
- Project rename / migration tooling
- `.ion-mem/config.json` schema versioning beyond a single `project` field
- Caching detection results across calls
- Watching `.ion-mem/config.json` for changes
- MCP integration (`mem_current_project` lives in `mcp-mvp`)
