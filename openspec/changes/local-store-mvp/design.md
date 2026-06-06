# Design: local-store-mvp

**Change**: `local-store-mvp`
**Module**: `github.com/ionix/ion-mem`
**Package**: `internal/store`
**Status**: Designed (awaiting `sdd-spec` / `sdd-tasks`)
**Strict TDD**: ACTIVE for this change (red → green → refactor on every behavior)
**Artifact store**: hybrid (this file + Engram `sdd/local-store-mvp/design`)

---

## 1. Overview

We are landing the first behavioral subsystem of `ion-mem`: a pure-Go,
embedded SQLite store with FTS5 search, soft-delete, content-hash dedupe,
and topic-key upsert. After this change, every later layer (MCP, HTTP, sync,
CLI, TUI) sits on top of a single, tested package: `internal/store`.

Local-first SQLite + FTS5 is the right shape because (a) it matches the
upstream engram philosophy we're forking — a single embedded file owned by
the user with full-text search built in; (b) it lets the local layer ship
as a single static binary with no external runtime dependency (no Postgres,
no Elastic); and (c) FTS5 BM25 ranking gives us competitive search quality
without an LLM in the hot path. The cloud layer (separate change) will sync
on top of this, never replace it.

The design is intentionally a behavioral subset of upstream `engram/internal/store`.
We re-implement against upstream as a reference shape; we do not vendor or
copy code. Schema parity is preserved on the in-scope tables so future
upstream cherry-picks remain mechanically possible.

---

## 2. Resolved decisions (8 open questions locked)

| # | Question | Decision | Justification |
|---|----------|----------|---------------|
| 1 | SQLite driver | `modernc.org/sqlite v1.45.0` (pinned) | Pure Go (no cgo), matches upstream `engram-source/go.mod` line 13 exactly. Cross-compile stays trivial. |
| 2 | Test library | stdlib `testing` + small helpers (`store_helpers_test.go`) | Zero dep weight, matches upstream. Helpers (`mustOpen`, `mustAdd`) keep table tests readable. |
| 3 | Schema versioning | `schema_version` table + linear Go migration funcs | No extra dep. Each slice owns one migration func keyed by version. Matches engram's idempotent `IF NOT EXISTS` style. |
| 4 | `Store` interface | Concrete `*Store` only for v1 | Extracting an interface before a second caller exists is premature. MCP/server changes can add one when they actually need a fake. |
| 5 | Session deletion | Sentinel `ErrSessionHasObservations` (409-equivalent), no cascade | Safer at the storage layer. Cascade lives in a future command-level helper. Mirrors `engram-source/internal/store/store.go:49`. |
| 6 | Dedup hash | SHA-256 over normalized content | Stdlib `crypto/sha256`, matches upstream. Normalization = lowercase + collapse whitespace; key = `hash + project + scope + type + title`. |
| 7 | `scope` default | `project`. Allowed: `project`, `personal`, `global`. | Mirrors engram's `scope TEXT NOT NULL DEFAULT 'project'` (upstream line 687). |
| 8 | Time storage | TEXT ISO-8601 (`time.RFC3339Nano`) with timezone | Engram parity. Human-readable in SQL shell, no locale traps, sortable as text. |

No design deviations from upstream on the in-scope subset.

---

## 3. Architecture

### 3.1 Package layout

```
internal/store/
├── doc.go                       # pre-existing from scaffold
├── store.go                     # *Store type, Open/Close, pragma setup, migrate dispatch
├── migrations.go                # schema_version table + linear migration runner
├── schema_0001_sessions.go      # Slice 1: sessions DDL (Go string constant + apply func)
├── schema_0002_observations.go  # Slice 2: observations + FTS + triggers + indexes
├── schema_0003_prompts.go       # Slice 3: user_prompts + FTS + triggers + indexes
├── sessions.go                  # Slice 1: CreateSession, EndSession, GetSession, RecentSessions, DeleteSession
├── observations.go              # Slice 2: Add/Update/Get/Recent/Delete/Search + dedup + topic-key upsert
├── prompts.go                   # Slice 3: AddPromptIfMissing, RecentPrompts, SearchPrompts, DeletePrompt
├── timeline.go                  # Slice 3: Timeline + Stats
├── helpers.go                   # Slice 1: time helpers, normalization, sha256 dedup key, sanitizeFTS
├── errors.go                    # Slice 1: sentinel Err* values
├── store_test.go                # Slice 1: black-box tests for Open/Close/migration
├── store_helpers_test.go        # Slice 1: test helpers (mustOpen, mustAdd) shared across slices
├── sessions_test.go             # Slice 1
├── observations_test.go         # Slice 2
├── search_test.go               # Slice 2: BM25, tokenization
├── prompts_test.go              # Slice 3
└── timeline_test.go             # Slice 3
```

**Decision on migration format**: Go string constants embedded in
`schema_NNNN_*.go` files (NOT separate `.sql` files with `//go:embed`).
Justification: avoids `embed.FS` ceremony for ~3 short scripts, keeps each
slice's PR diff in pure Go, and matches the upstream engram pattern where
DDL is inline.

### 3.2 File responsibilities (one line each)

| File | Responsibility |
|------|----------------|
| `store.go` | `Store` struct, `Open(dataDir string) (*Store, error)`, `(*Store).Close() error`, pragma application |
| `migrations.go` | `schema_version` table; `migrate(db)` runs registered migrations in order; idempotent |
| `schema_0001_sessions.go` | `sessions` DDL + apply func `applyMigration0001` |
| `schema_0002_observations.go` | `observations` + `observations_fts` + 3 triggers + indexes + apply func |
| `schema_0003_prompts.go` | `user_prompts` + `prompts_fts` + 3 triggers + indexes + apply func |
| `sessions.go` | Session CRUD; emits `ErrSessionHasObservations` on delete with children |
| `observations.go` | Observation CRUD, FTS5 BM25 search, dedup, topic-key upsert, soft-delete-aware reads |
| `prompts.go` | Prompt add-if-missing (session+content-hash dedup), recent, search, delete |
| `timeline.go` | Anchor-relative session timeline; aggregate `Stats` |
| `helpers.go` | `now() string` (RFC3339Nano), `normalizedHash(...)`, `sanitizeFTS(query)`, `normalizeScope`, `normalizeTopicKey` |
| `errors.go` | `ErrSessionNotFound`, `ErrSessionHasObservations`, `ErrObservationNotFound`, `ErrPromptNotFound`, `ErrNotFound` |

### 3.3 Dependency injection

```go
// Open constructs a Store rooted at dataDir. dataDir is created if missing.
// dbPath = filepath.Join(dataDir, "ion-mem.db").
func Open(dataDir string) (*Store, error)

type Store struct {
    db *sql.DB
}
```

- `dataDir` is mandatory and MUST be absolute (matches upstream).
- `sql.DB` is constructed internally via `sql.Open("sqlite", dbPath)` with
  `db.SetMaxOpenConns(1)` (engram parity; SQLite WAL still serializes writers).
- No constructor variant takes a `*sql.DB` in v1. Tests use `t.TempDir()` +
  `Open(...)`. If a future MCP/server test needs a fake, we extract an
  interface then — not now.

---

## 4. Data model

### 4.1 `schema_version` (Slice 1)

```sql
CREATE TABLE IF NOT EXISTS schema_version (
    version    INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL DEFAULT (datetime('now'))
);
```

Migration runner inserts one row per successfully applied migration. Re-running
is a no-op (`INSERT OR IGNORE`).

### 4.2 `sessions` (Slice 1)

```sql
CREATE TABLE IF NOT EXISTS sessions (
    id         TEXT PRIMARY KEY,
    project    TEXT NOT NULL,
    directory  TEXT NOT NULL,
    started_at TEXT NOT NULL DEFAULT (datetime('now')),
    ended_at   TEXT,
    summary    TEXT
);
CREATE INDEX IF NOT EXISTS idx_sessions_project ON sessions(project);
CREATE INDEX IF NOT EXISTS idx_sessions_started ON sessions(started_at DESC);
```

No foreign keys (root table).

### 4.3 `observations` + `observations_fts` (Slice 2)

```sql
CREATE TABLE IF NOT EXISTS observations (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    sync_id         TEXT,
    session_id      TEXT    NOT NULL,
    type            TEXT    NOT NULL,
    title           TEXT    NOT NULL,
    content         TEXT    NOT NULL,
    tool_name       TEXT,
    project         TEXT,
    scope           TEXT    NOT NULL DEFAULT 'project',
    topic_key       TEXT,
    normalized_hash TEXT,
    revision_count  INTEGER NOT NULL DEFAULT 1,
    duplicate_count INTEGER NOT NULL DEFAULT 1,
    last_seen_at    TEXT,
    created_at      TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at      TEXT    NOT NULL DEFAULT (datetime('now')),
    deleted_at      TEXT,
    FOREIGN KEY (session_id) REFERENCES sessions(id)
);

CREATE INDEX IF NOT EXISTS idx_obs_session ON observations(session_id);
CREATE INDEX IF NOT EXISTS idx_obs_type    ON observations(type);
CREATE INDEX IF NOT EXISTS idx_obs_project ON observations(project);
CREATE INDEX IF NOT EXISTS idx_obs_scope   ON observations(scope);
CREATE INDEX IF NOT EXISTS idx_obs_created ON observations(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_obs_deleted ON observations(deleted_at);
CREATE INDEX IF NOT EXISTS idx_obs_topic   ON observations(topic_key, project, scope, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_obs_dedupe  ON observations(normalized_hash, project, scope, type, title, created_at DESC);

CREATE VIRTUAL TABLE IF NOT EXISTS observations_fts USING fts5(
    title, content, tool_name, type, project, topic_key,
    content='observations',
    content_rowid='id',
    tokenize='unicode61 remove_diacritics 2'
);
```

Three triggers keep the FTS table coherent (`AFTER INSERT`, `AFTER DELETE`,
`AFTER UPDATE`). Pattern mirrors upstream `store.go:984-1005`.

### 4.4 `user_prompts` + `prompts_fts` (Slice 3)

```sql
CREATE TABLE IF NOT EXISTS user_prompts (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    sync_id    TEXT,
    session_id TEXT    NOT NULL,
    content    TEXT    NOT NULL,
    project    TEXT,
    created_at TEXT    NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (session_id) REFERENCES sessions(id)
);
CREATE INDEX IF NOT EXISTS idx_prompts_session ON user_prompts(session_id);
CREATE INDEX IF NOT EXISTS idx_prompts_project ON user_prompts(project);
CREATE INDEX IF NOT EXISTS idx_prompts_created ON user_prompts(created_at DESC);

CREATE VIRTUAL TABLE IF NOT EXISTS prompts_fts USING fts5(
    content, project,
    content='user_prompts',
    content_rowid='id',
    tokenize='unicode61 remove_diacritics 2'
);
```

Three triggers analogous to observations.

### 4.5 Foreign-key strategy

| Table | FK | ON DELETE |
|-------|----|-----------|
| `observations.session_id` | `sessions(id)` | RESTRICT (default); deletion blocked by `ErrSessionHasObservations` |
| `user_prompts.session_id` | `sessions(id)` | RESTRICT (default); deletion blocked by `ErrSessionHasObservations` |

No `ON DELETE CASCADE` in v1. PRAGMA `foreign_keys = ON` enforces RESTRICT.

### 4.6 Hot-path indexes

- `idx_obs_topic`: topic-key upsert lookup
- `idx_obs_dedupe`: dedup probe by `(normalized_hash, project, scope, type, title)`
- `idx_obs_deleted`: fast soft-delete filtering
- `idx_obs_created DESC`: `RecentObservations` ordering

---

## 5. Slice boundaries

Three stacked-to-main PRs. Each PR is buildable + testable + mergeable on its own.

### Slice 1 — `local-store-schema-sessions`

| Adds | Files |
|------|-------|
| Store skeleton | `store.go`, `migrations.go`, `errors.go`, `helpers.go` |
| First migration | `schema_0001_sessions.go` (creates `schema_version` + `sessions`) |
| Session ops | `sessions.go` |
| Tests | `store_test.go`, `store_helpers_test.go`, `sessions_test.go` |

**Acceptance gate**: `go build ./... && go test ./internal/store/... && gofmt -l . && go vet ./...` all exit 0. `go.mod` gets `modernc.org/sqlite v1.45.0` (first non-empty `require`).

### Slice 2 — `local-store-observations`

| Adds | Files |
|------|-------|
| Second migration | `schema_0002_observations.go` (observations + FTS + triggers + indexes) |
| Observation ops | `observations.go` |
| Tests | `observations_test.go`, `search_test.go` |

**Acceptance gate**: same commands exit 0. New tests cover dedup, topic upsert, soft-delete filtering, BM25 ranking, FTS5 tokenization on kebab-case topic keys, FK enforcement (`ErrSessionHasObservations`).

### Slice 3 — `local-store-prompts-stats`

| Adds | Files |
|------|-------|
| Third migration | `schema_0003_prompts.go` (user_prompts + FTS + triggers + indexes) |
| Prompt + timeline ops | `prompts.go`, `timeline.go` |
| Tests | `prompts_test.go`, `timeline_test.go` |

**Acceptance gate**: same commands exit 0. New tests cover prompt session-content dedup, prompt search, anchor timeline before/after, Stats counts per project/type/scope.

### Slice independence

- Slice 1 ships fully self-contained (no observations/prompts referenced).
- Slice 2 depends only on Slice 1's `sessions` table for FK target.
- Slice 3 depends only on Slice 1's `sessions` table for FK target.

Migration runner replays missing migrations on every `Open`, so an installation that already ran Slice 1 will pick up Slice 2's migration on first start after upgrade. No data backfill needed (additive schema).

---

## 6. Shared utilities (all land in Slice 1)

### Time

```go
// helpers.go
func nowISO() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func parseISO(s string) (time.Time, error) { return time.Parse(time.RFC3339Nano, s) }
```

All writes use `nowISO()`. SQLite `datetime('now')` defaults are kept on schema for safety, but Go-side writes always pass an explicit timestamp.

### Dedup hash

```go
// helpers.go
func normalizedHash(content string) string {
    n := strings.ToLower(strings.Join(strings.Fields(content), " "))
    sum := sha256.Sum256([]byte(n))
    return hex.EncodeToString(sum[:])
}
```

Dedup KEY (composite, not part of hash) = `(normalized_hash, project, scope, type, title)`. This composite key is the `idx_obs_dedupe` index in §4.3.

### FTS5 query sanitization

```go
// helpers.go
func sanitizeFTS(query string) string {
    // Wrap each whitespace-split term in double quotes to avoid FTS5 syntax errors on special chars.
    // Preserves kebab-case and dotted identifiers as single tokens.
}
```

Used by `Search` and `SearchPrompts`. Mirrors upstream `sanitizeFTS` (line 6225 of `engram-source/internal/store/store.go`).

### Scope normalization

```go
func normalizeScope(s string) string {
    s = strings.TrimSpace(strings.ToLower(s))
    switch s {
    case "personal", "global":
        return s
    default:
        return "project"
    }
}
```

---

## 7. Test strategy

### 7.1 Black-box discipline

All tests live in `package store_test` (NOT `package store`). They exercise only the exported API. This enforces that the public surface is sufficient for downstream callers.

### 7.2 Fixture setup

Each test opens a fresh DB:

```go
// store_helpers_test.go
func mustOpen(t *testing.T) *store.Store {
    t.Helper()
    s, err := store.Open(t.TempDir())
    if err != nil { t.Fatal(err) }
    t.Cleanup(func() { s.Close() })
    return s
}
```

`t.TempDir()` is used unconditionally so WAL files land on real disk (catches WAL-specific bugs). Pure in-memory mode (`:memory:`) is NOT used in v1 — it hides FK and WAL behaviors we care about.

### 7.3 Test coverage by slice

| Slice | Test file | Scenarios |
|-------|-----------|-----------|
| 1 | `store_test.go` | `Open` creates `schema_version` row; `Open` is idempotent on existing DB; `Close` is safe twice; `Open` rejects relative `dataDir` |
| 1 | `sessions_test.go` | `CreateSession` round-trips; `EndSession` sets `ended_at`; `RecentSessions` ordered by `started_at DESC`; `DeleteSession` returns `ErrSessionNotFound` for missing |
| 2 | `observations_test.go` | Add round-trip; dedup increments `duplicate_count` + bumps `last_seen_at`; topic-key upsert increments `revision_count`; soft delete excludes from `RecentObservations`; hard delete removes row + FTS entry; `DeleteSession` returns `ErrSessionHasObservations` when children exist |
| 2 | `search_test.go` | BM25 ranks more-relevant docs higher; kebab-case topic key (`sdd/local-store-mvp/design`) is findable as fragment; multi-word query works; soft-deleted rows excluded; project/type/scope filters honored |
| 3 | `prompts_test.go` | `AddPromptIfMissing` dedups same content in same session; `SearchPrompts` ranks; `DeletePrompt` returns `ErrPromptNotFound` for missing |
| 3 | `timeline_test.go` | `Timeline` returns anchor + before/after in session; `Stats` counts per project/type/scope |

### 7.4 Migration tests

In `store_test.go`:

- Open empty DB → all expected tables, indexes, FTS virtuals exist (verify via `sqlite_master`).
- Open existing DB → migration is idempotent (no error, no duplicate rows in `schema_version`).
- After Slice 2 lands, re-opening a Slice-1 DB applies migration 2 cleanly.

### 7.5 FTS5 tokenization tests

Explicit cases (Slice 2 `search_test.go`):

- `topic_key = "sdd/local-store-mvp/design"` is found by query `"local-store-mvp"`.
- `topic_key = "architecture/auth-model"` is found by query `"auth"` (kebab fragment).
- Multi-word query `"sqlite migration"` ranks rows containing both terms higher.

### 7.6 Concurrency

NOT in MVP. The proposal calls for a `SQLITE_BUSY` busy-retry test (§3.3 item 10), which is straightforward (start two writes against same DB). We will include ONE concurrency test in Slice 2: two goroutines adding observations concurrently must both succeed (busy_timeout + WAL handles it). Full concurrency test matrix is deferred.

---

## 8. Strict TDD operational notes

This change activates Strict TDD for `ion-mem`. `sdd-apply` MUST:

1. **Red**: Write a failing test before any production code.
2. **Green**: Write the minimum production code to make it pass.
3. **Refactor**: Clean up while green.

### First test (Slice 1, mandatory starting point)

```go
// store_test.go — the first test that exists in the repo
func TestOpen_CreatesSchemaVersionRow(t *testing.T) {
    s := mustOpen(t)
    var version int
    err := s.DB().QueryRow("SELECT version FROM schema_version ORDER BY version DESC LIMIT 1").Scan(&version)
    if err != nil { t.Fatalf("schema_version row not found: %v", err) }
    if version < 1 { t.Fatalf("expected schema_version >= 1, got %d", version) }
}
```

(If exposing `DB()` is too much surface area, the test can instead assert via a public `SchemaVersion() int` accessor — decide during apply.)

The `sdd-apply` agent receives `strict_tdd: true` from the orchestrator after this design lands. No production code should be written without a failing test first.

---

## 9. Open questions (post-design)

None. All 8 proposal-level open questions are resolved in §2. No new architectural ambiguities surfaced during design.

One operational nit deferred to `sdd-apply`: whether to expose `(*Store).DB() *sql.DB` for tests or use a `SchemaVersion()` accessor. Both are valid; default to `SchemaVersion()` accessor (smaller surface area) and only expose `DB()` if a test genuinely needs it.

---

## 10. Risks (architectural)

| Risk | Mitigation |
|------|------------|
| `modernc.org/sqlite` perf gap vs cgo `mattn/go-sqlite3` | Acceptable at MVP scale (single-user, low write rate). Revisit if benchmarks show a real problem post-MVP. |
| FTS5 tokenization surprises on kebab-case topic keys | Explicit tests in §7.5 lock the contract. `unicode61 remove_diacritics 2` is the same tokenizer upstream uses. |
| Schema drift vs upstream engram | Document any divergence in the `local-store` capability spec when it's archived. No divergence introduced in this change. |
| Strict TDD friction on first behavioral change | Acknowledged. This is the right moment to set the discipline — every later change inherits it for free. |
| Per-slice migrations could drift if a later slice forgets to register | Migration runner enforces sequential application by version number; a missing migration is a build-time obvious gap. Tests in §7.4 catch it on every CI run. |
| Soft-delete leak through a read path | Every read path gets a "deleted row excluded" assertion in its slice's tests. |

---

## 11. Pointers to upstream (reference only — DO NOT copy)

| Concern | Upstream location |
|---------|-------------------|
| Driver version | `engram-source/go.mod:13` (`modernc.org/sqlite v1.45.0`) |
| Pragmas | `engram-source/internal/store/store.go:602-607` |
| Schema (sessions/observations/prompts) | `engram-source/internal/store/store.go:669-741` |
| FTS5 tokenizer + content table config | `engram-source/internal/store/store.go:704-713`, `:736-741` |
| FTS5 triggers | `engram-source/internal/store/store.go:984-1034` |
| Indexes | `engram-source/internal/store/store.go:699-702`, `:822-826` |
| Sentinel errors | `engram-source/internal/store/store.go:47-54` |
| `sanitizeFTS` | `engram-source/internal/store/store.go:6225` |

Use these for shape only. Re-implement, don't copy.

---

## 12. Next step

Ready for `sdd-spec` (capability contract) and then `sdd-tasks` (mechanical breakdown of Slice 1 → Slice 2 → Slice 3, strict-TDD ordered).
