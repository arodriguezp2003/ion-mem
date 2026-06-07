# Spec: local-store-mvp

**Change**: `local-store-mvp`
**Capability**: `local-store`
**Package**: `internal/store`
**Strict TDD**: ACTIVE — every behavior introduced via red → green → refactor
**Delivery**: 3 stacked-to-main PRs (Slice 1 → 2 → 3)
**Status**: Ready for `sdd-tasks`

---

## 1. Capability: local-store

### 1.1 Summary

`local-store` is a pure-Go, embedded SQLite + FTS5 persistence layer in
`internal/store`. It is the source of truth for the local memory engine:
sessions group observations and prompts chronologically; observations carry
type, title, content, and optional tool attribution with content-hash
deduplication and topic-key upsert semantics; user prompts are stored
with session-scoped deduplication. Every read operation excludes
soft-deleted rows by default. Full-text search uses FTS5 BM25 ranking with
`unicode61 remove_diacritics 2` tokenization. The store is local-first: a
single embedded SQLite file (`ion-mem.db`) owned by the user, opened via
`Open(dataDir string) (*Store, error)`, and closed via `(*Store).Close()`.
No network dependency exists in this layer. The schema is versioned via a
`schema_version` table and applied via linear Go migration functions — one
per slice — on every `Open` call (idempotent).

---

## 2. Slice 1 — Schema + Sessions

### 2.1 Requirements

| # | Verb | Requirement |
|---|------|-------------|
| S1-R01 | MUST | `go.mod` lists `modernc.org/sqlite v1.45.0` as the sole non-empty direct dependency. |
| S1-R02 | MUST | `Open(dataDir string) (*Store, error)` opens `<dataDir>/ion-mem.db`, creates `dataDir` (and any missing parent directories) if absent, applies all pending migrations, and returns a ready `*Store`. |
| S1-R03 | MUST | `(*Store).Close() error` closes the underlying `*sql.DB` and releases file locks. Calling `Close` twice MUST NOT panic; the second call MAY return an error. |
| S1-R04 | MUST | `Open` applies these SQLite pragmas in order before migrations: `journal_mode=WAL`, `busy_timeout=5000`, `foreign_keys=ON`, `synchronous=NORMAL`. |
| S1-R05 | MUST | `Open` sets `db.SetMaxOpenConns(1)` on the internal `*sql.DB`. |
| S1-R06 | MUST | A `schema_version` table exists with columns `version INTEGER PRIMARY KEY` and `applied_at TEXT NOT NULL`. It holds one row per successfully applied migration. |
| S1-R07 | MUST | Migration 0001 creates a `sessions` table with columns: `id TEXT PRIMARY KEY`, `project TEXT NOT NULL`, `directory TEXT NOT NULL`, `started_at TEXT NOT NULL`, `ended_at TEXT NULL`, `summary TEXT NULL`, `status TEXT NOT NULL DEFAULT 'active'`. |
| S1-R08 | MUST | Migration 0001 creates `idx_sessions_project ON sessions(project)` and `idx_sessions_started ON sessions(started_at DESC)`. |
| S1-R09 | MUST | `CreateSession(ctx context.Context, params CreateSessionParams) (Session, error)` inserts one row. `params` carries `ID` (caller-provided), `Project`, `Directory`. The returned `Session` has `StartedAt` = current UTC time (RFC3339Nano), `Status` = `"active"`. |
| S1-R10 | MUST | `CreateSession` returns a non-nil error (wrapping the driver's primary-key error) when called with an ID that already exists. |
| S1-R11 | MUST | `EndSession(ctx context.Context, sessionID, summary string) error` sets `ended_at = nowISO()`, `status = "ended"`, and `summary = summary` on the matching row. |
| S1-R12 | MUST | `EndSession` is idempotent: calling it on an already-ended session MUST update `ended_at` and `summary` again (last-write wins) and return nil. |
| S1-R13 | MUST | `EndSession` returns `ErrNotFound` (via `errors.Is`) when `sessionID` does not exist. |
| S1-R14 | MUST | `GetSession(ctx context.Context, sessionID string) (Session, error)` returns the session or `ErrNotFound`. |
| S1-R15 | MUST | `RecentSessions(ctx context.Context, project string, limit int) ([]Session, error)` returns sessions ordered by `started_at DESC`. When `project` is empty it returns sessions across all projects. When `limit <= 0` it defaults to 50. |
| S1-R16 | MUST | `DeleteSession(ctx context.Context, sessionID string) error` deletes the session row. Returns `ErrSessionHasObservations` when at least one child observation or prompt FK-references the session. Returns `ErrNotFound` when `sessionID` does not exist. |
| S1-R17 | MUST | Sentinel errors in `errors.go`: `ErrNotFound`, `ErrSessionHasObservations`, `ErrObservationNotFound`, `ErrPromptNotFound`, `ErrMigrationFailed`. All are exported values compatible with `errors.Is`. |
| S1-R18 | MUST | `dataDir` passed to `Open` MUST be an absolute path. `Open` returns a non-nil error when given a relative path. |
| S1-R19 | MUST | `Open` returns a non-nil error when `dataDir` exists as a regular file (not a directory). |
| S1-R20 | SHOULD | All `Session` fields that correspond to nullable SQLite columns (`EndedAt`, `Summary`) use pointer types (`*string` or `*time.Time`) so nil represents SQL NULL. |

### 2.2 Scenarios

#### S1-T01 — Open creates DB file and applies migration 0001 on a fresh data dir
```
Given  a temporary directory that contains no files
When   store.Open(tmpDir) is called
Then   ion-mem.db exists inside tmpDir
And    a query on schema_version returns at least one row with version=1
And    a query on sqlite_master confirms sessions table exists
And    the returned error is nil
```

#### S1-T02 — Open is idempotent (no duplicate schema_version rows)
```
Given  a store opened and closed against a temp dir
When   store.Open is called again on the same temp dir
Then   schema_version contains exactly one row with version=1
And    the returned error is nil
```

#### S1-T03 — Open returns error when dataDir is a regular file
```
Given  a path that points to an existing regular file
When   store.Open(filePath) is called
Then   a non-nil error is returned
And    no ion-mem.db is created at that path
```

#### S1-T04 — Open returns error on relative dataDir
```
Given  a relative path string ("./data")
When   store.Open("./data") is called
Then   a non-nil error is returned
```

#### S1-T05 — WAL mode is active after Open
```
Given  an open Store
When   PRAGMA journal_mode is queried
Then   the result is "wal"
```

#### S1-T06 — busy_timeout is set after Open
```
Given  an open Store
When   PRAGMA busy_timeout is queried
Then   the result is 5000
```

#### S1-T07 — foreign_keys pragma is ON after Open
```
Given  an open Store
When   PRAGMA foreign_keys is queried
Then   the result is 1
```

#### S1-T08 — CreateSession + GetSession round-trip
```
Given  an open Store
When   CreateSession is called with id="s1", project="ionix", directory="/tmp/ionix"
And    GetSession is called with id="s1"
Then   the returned Session has ID="s1", Project="ionix", Directory="/tmp/ionix"
And    Status="active", StartedAt is a valid RFC3339Nano timestamp, EndedAt is nil
```

#### S1-T09 — CreateSession duplicate ID returns error
```
Given  an open Store with session id="s1" already created
When   CreateSession is called again with id="s1"
Then   a non-nil error is returned
And    the sessions table still contains exactly one row with id="s1"
```

#### S1-T10 — EndSession sets ended_at and status
```
Given  an active session with id="s1"
When   EndSession(ctx, "s1", "summary text") is called
Then   GetSession returns a Session with Status="ended", EndedAt non-nil, Summary="summary text"
```

#### S1-T11 — EndSession on unknown session returns ErrNotFound
```
Given  an open Store with no session "missing"
When   EndSession(ctx, "missing", "") is called
Then   errors.Is(err, store.ErrNotFound) is true
```

#### S1-T12 — RecentSessions respects DESC ordering and limit
```
Given  sessions created with started_at values T1 < T2 < T3
When   RecentSessions(ctx, "", 2) is called
Then   exactly 2 sessions are returned, with the T3 session first
```

#### S1-T13 — RecentSessions with empty project returns from all projects
```
Given  sessions in project "A" and project "B"
When   RecentSessions(ctx, "", 50) is called
Then   sessions from both projects are returned
```

#### S1-T14 — DeleteSession with no observations succeeds
```
Given  an active session with no child observations or prompts
When   DeleteSession(ctx, sessionID) is called
Then   err is nil
And    GetSession returns ErrNotFound
```

#### S1-T15 — DeleteSession with observations returns ErrSessionHasObservations
```
Given  a session with at least one child observation
When   DeleteSession(ctx, sessionID) is called
Then   errors.Is(err, store.ErrSessionHasObservations) is true
And    the session row still exists
```

#### S1-T16 — DeleteSession on unknown ID returns ErrNotFound
```
Given  an open Store with no session "ghost"
When   DeleteSession(ctx, "ghost") is called
Then   errors.Is(err, store.ErrNotFound) is true
```

### 2.3 Acceptance criteria (Slice 1)

- [x] `go test ./internal/store/...` exits 0 with no skipped tests and no `t.Skip` calls
- [x] Slice 1 covers at least 12 distinct test cases (table-driven tests count per row)
- [x] `go vet ./...` exits 0
- [x] `gofmt -l .` produces no output
- [x] `go.mod` lists `modernc.org/sqlite v1.45.0` as the only direct dependency
- [x] CI green on the slice's PR before Slice 2 begins

---

## 3. Slice 2 — Observations + FTS5 + Search

### 3.1 Requirements

| # | Verb | Requirement |
|---|------|-------------|
| S2-R01 | MUST | Migration 0002 creates the `observations` table with columns: `id INTEGER PRIMARY KEY AUTOINCREMENT`, `sync_id TEXT NOT NULL UNIQUE`, `session_id TEXT NOT NULL REFERENCES sessions(id)`, `type TEXT NOT NULL`, `title TEXT NOT NULL`, `content TEXT NOT NULL`, `tool_name TEXT NULL`, `project TEXT NOT NULL`, `scope TEXT NOT NULL DEFAULT 'project'`, `topic_key TEXT NULL`, `normalized_hash TEXT NOT NULL`, `revision_count INTEGER NOT NULL DEFAULT 1`, `duplicate_count INTEGER NOT NULL DEFAULT 0`, `last_seen_at TEXT NOT NULL`, `created_at TEXT NOT NULL`, `updated_at TEXT NOT NULL`, `deleted_at TEXT NULL`. |
| S2-R02 | MUST | Migration 0002 creates `observations_fts` as a FTS5 content table over `observations` with columns `title, content, tool_name, type, project, topic_key`, `content_rowid='id'`, and `tokenize='unicode61 remove_diacritics 2'`. |
| S2-R03 | MUST | Migration 0002 creates three FTS5 sync triggers on `observations`: `AFTER INSERT` (insert into FTS), `AFTER DELETE` (delete from FTS), `AFTER UPDATE` (delete old + insert new). |
| S2-R04 | MUST | Migration 0002 creates indexes: `idx_obs_session(session_id)`, `idx_obs_type(type)`, `idx_obs_project(project)`, `idx_obs_scope(scope)`, `idx_obs_created(created_at DESC)`, `idx_obs_deleted(deleted_at)`, `idx_obs_topic(topic_key, project, scope, updated_at DESC)`, `idx_obs_dedupe(normalized_hash, project, scope, type, title, created_at DESC)`. |
| S2-R05 | MUST | `AddObservation(ctx context.Context, params AddObservationParams) (Observation, error)` accepts: `SessionID` (required), `Type`, `Title`, `Content`, `ToolName` (optional), `Project`, `Scope` (defaults to `"project"` if empty), `TopicKey` (optional). |
| S2-R06 | MUST | `AddObservation` computes `normalized_hash` as SHA-256 of `strings.ToLower(strings.Join(strings.Fields(content), " "))`. |
| S2-R07 | MUST | `AddObservation` deduplication rule: if a non-deleted row with matching `(normalized_hash, project, scope, type, title)` already exists, increment `duplicate_count`, update `last_seen_at = nowISO()` and `updated_at = nowISO()` on the existing row, and return that existing observation without inserting. |
| S2-R08 | MUST | `AddObservation` topic-key upsert rule: if `TopicKey` is non-empty AND a non-deleted row with matching `(project, scope, topic_key)` exists, UPDATE that row in place (title, content, type, normalized_hash, last_seen_at, updated_at), increment `revision_count`, and return the updated observation. Topic-key upsert takes precedence over dedup (checked first). |
| S2-R09 | MUST | `AddObservation` new-row rule: if neither dedup nor topic-key upsert matches, INSERT a new row. `sync_id` is `"obs-" + hex(crypto/rand 8 bytes)`. `revision_count = 1`, `duplicate_count = 0`. |
| S2-R10 | MUST | `AddObservation` returns a non-nil error (wrapping the FK driver error) when `SessionID` does not reference an existing session. |
| S2-R11 | MUST | `UpdateObservation(ctx context.Context, id int64, params UpdateObservationParams) (Observation, error)` applies a partial update: only non-nil fields in `params` are changed. Always updates `updated_at`. Returns `ErrObservationNotFound` if `id` does not exist. |
| S2-R12 | MUST | `GetObservation(ctx context.Context, id int64) (Observation, error)` returns the observation or `ErrObservationNotFound`. Soft-deleted observations are NOT returned by default. |
| S2-R13 | SHOULD | A `GetObservationIncludingDeleted(ctx context.Context, id int64) (Observation, error)` function (or equivalent option) exists to allow retrieval of soft-deleted observations. |
| S2-R14 | MUST | `RecentObservations(ctx context.Context, params RecentObservationsParams) ([]Observation, error)` returns non-deleted observations ordered by `created_at DESC`. Params: `Project` (optional filter), `Scope` (optional filter), `Limit` (default 50 when <= 0). |
| S2-R15 | MUST | `DeleteObservation(ctx context.Context, id int64, hard bool) error` — when `hard=false`: sets `deleted_at = nowISO()` (soft delete). When `hard=true`: deletes the row and the corresponding FTS entry. Returns `ErrObservationNotFound` if `id` does not exist. |
| S2-R16 | MUST | `Search(ctx context.Context, params SearchParams) ([]SearchResult, error)` queries `observations_fts` using BM25 ranking. Params: `Q` (FTS5 query, sanitized via `sanitizeFTS`), `Type` (optional filter), `Project` (optional filter), `Scope` (optional filter), `Limit` (default 20 when <= 0). Soft-deleted observations are excluded. Returns empty slice (not error) when no results match. |
| S2-R17 | MUST | `SearchResult` carries the matched `Observation` and a `Score float64` (BM25, lower is more relevant in SQLite's implementation). |
| S2-R18 | MUST | Package-private helpers: `normalizeForHash(s string) string`, `computeDedupHash(content string) string`, `generateSyncID() string` (prefix `"obs-"`). These are in `helpers.go`. |
| S2-R19 | MUST | Migration 0002 applies cleanly on top of a database that has only migration 0001 applied. |
| S2-R20 | SHOULD | One concurrency test verifies that two goroutines calling `AddObservation` on the same store concurrently both succeed (WAL + busy_timeout handles contention). |

### 3.2 Scenarios

(Full scenarios defined in the spec — abbreviated here for archive brevity)

All 20 S2-T* scenarios from S2-T01 through S2-T20 are tested and passing.

### 3.3 Acceptance criteria (Slice 2)

- [x] All Slice 1 tests still pass (no regression)
- [x] `go test ./internal/store/...` exits 0 with no skipped tests
- [x] Slice 2 adds at least 20 distinct test cases (S2-T01 through S2-T20 minimum)
- [x] FTS5 tokenization tests (S2-T13 and S2-T14) are present and document expected behavior in test names
- [x] `go vet ./...` and `gofmt -l .` clean
- [x] CI green on the slice's PR before Slice 3 begins

---

## 4. Slice 3 — Prompts + Timeline + Stats

### 4.1 Requirements

| # | Verb | Requirement |
|---|------|-------------|
| S3-R01 | MUST | Migration 0003 creates `user_prompts` table with columns: `id INTEGER PRIMARY KEY AUTOINCREMENT`, `sync_id TEXT NOT NULL UNIQUE`, `session_id TEXT NOT NULL REFERENCES sessions(id)`, `content TEXT NOT NULL`, `project TEXT NOT NULL`, `created_at TEXT NOT NULL`. |
| S3-R02 | MUST | Migration 0003 creates `prompts_fts` as a FTS5 content table over `user_prompts` with columns `content, project`, `content_rowid='id'`, `tokenize='unicode61 remove_diacritics 2'`. |
| S3-R03 | MUST | Migration 0003 creates three FTS5 sync triggers on `user_prompts`: `AFTER INSERT`, `AFTER DELETE`, `AFTER UPDATE`. |
| S3-R04 | MUST | Migration 0003 creates indexes: `idx_prompts_session(session_id)`, `idx_prompts_project(project)`, `idx_prompts_created(created_at DESC)`. |
| S3-R05 | MUST | `AddPromptIfMissing(ctx context.Context, params AddPromptParams) (Prompt, error)` deduplicates by SHA-256 of `(content + session_id)`. If an existing row matches the same session and content hash, return it without inserting. Otherwise INSERT a new row. `sync_id` prefix is `"pr-"`. |
| S3-R06 | MUST | `AddPromptIfMissing` returns a non-nil error when `params.SessionID` does not reference an existing session. |
| S3-R07 | MUST | `RecentPrompts(ctx context.Context, project string, limit int) ([]Prompt, error)` returns prompts ordered by `created_at DESC`. When `project` is empty returns from all projects. Default `limit` is 50 when `<= 0`. |
| S3-R08 | MUST | `SearchPrompts(ctx context.Context, params SearchPromptsParams) ([]Prompt, error)` queries `prompts_fts` using BM25. Params: `Q`, `Project` (optional), `Limit` (default 20). Returns empty slice (not error) on no matches. |
| S3-R09 | MUST | `DeletePrompt(ctx context.Context, id int64) error` removes the row and the FTS entry (hard delete only — prompts have no soft-delete). Returns `ErrPromptNotFound` when `id` does not exist. |
| S3-R10 | MUST | `Timeline(ctx context.Context, observationID int64, before, after int) ([]TimelineEntry, error)` returns observations and prompts in the same session as `observationID`, interleaved in chronological order, including `before` entries before and `after` entries after the anchor. |
| S3-R11 | MUST | `TimelineEntry` has a `Kind string` field (`"observation"` or `"prompt"`) and exactly one of `Observation *Observation` or `Prompt *Prompt` set (tagged union). |
| S3-R12 | MUST | `Timeline` excludes soft-deleted observations from results. |
| S3-R13 | MUST | `Timeline` returns `ErrObservationNotFound` when `observationID` does not exist or is soft-deleted. |
| S3-R14 | MUST | `Stats(ctx context.Context) (Stats, error)` returns aggregate counts: `TotalObservations int64` (non-deleted), `TotalPrompts int64`, `TotalSessions int64`, and `ByProject []ProjectStats` where each `ProjectStats` holds `Project string`, `ObservationCount int64`, `PromptCount int64`. |
| S3-R15 | MUST | Migration 0003 applies cleanly on top of a database that has migrations 0001 and 0002 applied. |
| S3-R16 | SHOULD | `DeleteSession` also blocks when at least one child prompt FK-references the session (returning `ErrSessionHasObservations`). |

### 4.2 Scenarios

All 17 S3-T* scenarios from S3-T01 through S3-T17 are tested and passing.

### 4.3 Acceptance criteria (Slice 3)

- [x] All Slice 1 + Slice 2 tests still pass (no regression)
- [x] `go test ./internal/store/...` exits 0 with no skipped tests
- [x] Slice 3 adds at least 17 distinct test cases (S3-T01 through S3-T17 minimum)
- [x] Timeline tests cover edge cases: start of session (S3-T13), mixed types (S3-T14), soft-delete exclusion (S3-T15)
- [x] `go vet ./...` and `gofmt -l .` clean
- [x] CI green on the slice's PR

---

## 5. Cross-cutting requirements

| # | Verb | Requirement |
|---|------|-------------|
| CC-R01 | MUST | Every public function accepts `context.Context` as its first parameter. |
| CC-R02 | MUST | Every public function returns `error` as its last return value. |
| CC-R03 | MUST | All context cancellation is honored: if `ctx.Err()` is non-nil before a DB call, the function returns the context error. |
| CC-R04 | MUST | No third-party dependencies beyond `modernc.org/sqlite v1.45.0`. No test assertion libraries. |
| CC-R05 | MUST | Test files end in `_test.go` and live in `internal/store/` alongside source files. |
| CC-R06 | MUST | Black-box tests use `package store_test`. White-box tests use `package store` only when private helpers must be tested directly. |
| CC-R07 | MUST | `mustOpen(t *testing.T) *store.Store` in `store_helpers_test.go` uses `t.TempDir()` and registers `t.Cleanup(func() { s.Close() })`. No `:memory:` DB in tests. |
| CC-R08 | MUST | All timestamps are written as RFC3339Nano UTC strings via `nowISO()`. SQLite column defaults (`datetime('now')`) are retained on schema but Go-side writes always pass an explicit timestamp. |
| CC-R09 | MUST | All exported types (`Session`, `Observation`, `Prompt`, `TimelineEntry`, `SearchResult`, `Stats`, `ProjectStats`) are defined in their slice's source file, not in `store.go`. |
| CC-R10 | SHOULD | Nil pointer fields in `Session` (e.g. `EndedAt *string`) represent SQL NULL. The store MUST NOT return a zero-value string where SQL stores NULL. |
| CC-R11 | MUST | Strict TDD discipline: every behavior in every slice is introduced via failing test → minimum production code → refactor. No production code is written without a failing test first. |

---

## 6. Out of scope

| Item | Deferred to |
|------|-------------|
| `memory_relations` table and conflict surfacing | `mcp-conflict-surfacing` or `cloud-mvp` |
| `sync_mutations` and cloud sync mutations queue | `cloud-mvp` |
| MCP server, HTTP API, CLI, TUI | Respective future changes |
| Project auto-detection | `project-detection` |
| Performance benchmarks | Dedicated future change |
| Concurrency stress tests (beyond the one WAL test in Slice 2) | Deferred |
| `mem_judge` / `mem_compare` semantics | Future change |
| LLM runners | Future change |
| Multi-database backends | Never (SQLite-only product decision) |
| `(*Store).DB() *sql.DB` accessor exposure | Deferred to apply decision (prefer `SchemaVersion()` accessor) |

---

## 7. Error sentinel reference

| Sentinel | Used when |
|----------|-----------|
| `ErrNotFound` | Session not found in `GetSession`, `EndSession`, `DeleteSession` |
| `ErrSessionHasObservations` | `DeleteSession` blocked by child observations or prompts |
| `ErrObservationNotFound` | `GetObservation`, `UpdateObservation`, `DeleteObservation`, `Timeline` anchor missing |
| `ErrPromptNotFound` | `DeletePrompt` target missing |
| `ErrMigrationFailed` | Migration runner encounters an unrecoverable schema error |

All sentinels MUST be comparable with `errors.Is`. All MUST be exported from `errors.go`.
