# Design: local-store-mvp

**Change**: `local-store-mvp`
**Module**: `github.com/ionix/ion-mem`
**Package**: `internal/store`
**Status**: Designed (awaiting `sdd-spec` / `sdd-tasks`)
**Strict TDD**: ACTIVE for this change
**File**: openspec/changes/local-store-mvp/design.md

## 1. Overview

First behavioral subsystem of `ion-mem`: pure-Go embedded SQLite store with FTS5 search, soft-delete, content-hash dedupe, topic-key upsert. Behavioral subset of upstream `engram/internal/store`, re-implemented (not vendored).

## 2. Resolved decisions (8 open questions LOCKED)

| # | Question | Decision |
|---|----------|----------|
| 1 | SQLite driver | `modernc.org/sqlite v1.45.0` (pinned, matches upstream go.mod:13) |
| 2 | Test library | stdlib `testing` + `store_helpers_test.go` helpers |
| 3 | Schema versioning | `schema_version` table + linear Go migration funcs, no library |
| 4 | `Store` interface | Concrete `*Store` only in v1 |
| 5 | Session deletion | `ErrSessionHasObservations` (RESTRICT FK), no cascade |
| 6 | Dedup hash | SHA-256 over normalized content (lowercase + collapse whitespace); key = (hash, project, scope, type, title) |
| 7 | `scope` default | `project`. Allowed: project, personal, global |
| 8 | Time storage | TEXT ISO-8601 `time.RFC3339Nano` UTC |

No upstream deviations.

## 3. Architecture

### Package layout (`internal/store/`)
- `store.go` — `*Store`, `Open(dataDir) (*Store, error)`, `Close()`, pragmas
- `migrations.go` — schema_version table + linear runner
- `schema_0001_sessions.go` — Slice 1 DDL (Go string constants, no embed.FS)
- `schema_0002_observations.go` — Slice 2 DDL
- `schema_0003_prompts.go` — Slice 3 DDL
- `sessions.go`, `observations.go`, `prompts.go`, `timeline.go` — operations per slice
- `helpers.go` — nowISO (RFC3339Nano), normalizedHash, sanitizeFTS, normalizeScope
- `errors.go` — sentinel `Err*` values
- Test files black-box in `package store_test`: `store_test.go`, `store_helpers_test.go`, `sessions_test.go`, `observations_test.go`, `search_test.go`, `prompts_test.go`, `timeline_test.go`

### DI
`Open(dataDir string)` only — `dataDir` must be absolute. Internal `sql.Open("sqlite", dbPath)` with `db.SetMaxOpenConns(1)`. No `*sql.DB` injection in v1.

## 4. Data model

- `schema_version(version PK, applied_at)` — Slice 1
- `sessions(id PK, project, directory, started_at, ended_at, summary)` + 2 indexes — Slice 1
- `observations(id PK, sync_id, session_id FK→sessions, type, title, content, tool_name, project, scope DEFAULT 'project', topic_key, normalized_hash, revision_count, duplicate_count, last_seen_at, created_at, updated_at, deleted_at)` + 8 indexes — Slice 2
- `observations_fts USING fts5(title, content, tool_name, type, project, topic_key, content='observations', content_rowid='id', tokenize='unicode61 remove_diacritics 2')` + 3 triggers (INSERT/UPDATE/DELETE) — Slice 2
- `user_prompts(id PK, sync_id, session_id FK→sessions, content, project, created_at)` + 3 indexes — Slice 3
- `prompts_fts USING fts5(content, project, ...)` + 3 triggers — Slice 3

FK strategy: RESTRICT only (PRAGMA foreign_keys=ON). No CASCADE in v1.

Pragmas (set in `Open`): `journal_mode=WAL`, `busy_timeout=5000`, `synchronous=NORMAL`, `foreign_keys=ON`.

## 5. Slice boundaries (3 stacked-to-main PRs)

**Slice 1 — `local-store-schema-sessions`** (~400-550 LOC)
- Adds: `store.go`, `migrations.go`, `errors.go`, `helpers.go`, `schema_0001_sessions.go`, `sessions.go`, `store_test.go`, `store_helpers_test.go`, `sessions_test.go`
- Adds dep: `modernc.org/sqlite v1.45.0`
- Acceptance: `go build/test/vet ./... && gofmt -l .` clean

**Slice 2 — `local-store-observations`** (~550-700 LOC)
- Adds: `schema_0002_observations.go`, `observations.go`, `observations_test.go`, `search_test.go`
- Tests: dedup, topic-key upsert, soft-delete filtering, BM25, kebab-case FTS tokenization, FK enforcement (`ErrSessionHasObservations`)

**Slice 3 — `local-store-prompts-stats`** (~350-500 LOC)
- Adds: `schema_0003_prompts.go`, `prompts.go`, `timeline.go`, `prompts_test.go`, `timeline_test.go`
- Tests: prompt dedup, search, anchor timeline, Stats counts

Migrations run on every `Open` (idempotent); a Slice-1 DB picks up Slice 2's migration on first open after upgrade.

## 6. Shared utilities (Slice 1)
- `nowISO() string` — `time.Now().UTC().Format(time.RFC3339Nano)`
- `normalizedHash(content) string` — sha256(lowercase + collapsed-whitespace)
- `sanitizeFTS(query) string` — wrap whitespace-split terms in quotes (mirrors upstream:6225)
- `normalizeScope(s) string` — project|personal|global, default project

## 7. Test strategy
- Black-box `package store_test`
- `mustOpen(t)` uses `t.TempDir()` (real disk for WAL/FK behavior); no `:memory:`
- Migration tests: empty DB → tables exist; existing DB → idempotent
- FTS5 tokenization: explicit kebab-case + dotted-key cases
- Concurrency: one busy-retry test in Slice 2; full matrix deferred

## 8. Strict TDD
First test (Slice 1): `TestOpen_CreatesSchemaVersionRow`. Apply agent receives `strict_tdd: true`; every behavior follows red → green → refactor.

## 9. Open questions
None blocking. Operational nit deferred to apply: `(*Store).DB()` vs `SchemaVersion()` accessor for tests — default to accessor.

## 10. Architectural risks
- modernc perf gap (acceptable at MVP scale)
- FTS5 tokenizer surprises (explicit tests lock contract)
- Schema drift vs upstream (none introduced; document any future divergence in capability spec)
- Strict TDD friction on first change (acknowledged, right moment)
- Per-slice migration drift (runner enforces sequential, tests catch gaps)
- Soft-delete leak (every read path gets exclusion assertion)

## 11. Upstream references (DO NOT copy)
- go.mod:13, store.go:602-607 (pragmas), :669-741 (schema), :704-713 + :736-741 (FTS), :984-1034 (triggers), :47-54 (errors), :6225 (sanitizeFTS)

## 12. Next step
`sdd-spec` (capability contract) → `sdd-tasks` (strict-TDD ordered breakdown).
