# Proposal: local-store-mvp

Ship the first real subsystem of `ion-mem`: a SQLite + FTS5 persistence layer
in `internal/store` that mirrors engram's local store behavior on a deliberately
narrow subset. After this change, the project owns a tested, embeddable memory
engine that every later layer (MCP server, HTTP API, cloud sync, TUI) will sit
on top of.

---

## 1. Intent

Stand up `internal/store` as a pure-Go, embedded SQLite store with FTS5
full-text search, soft-delete semantics, content-hash deduplication, and
topic-key upsert. This is the first behavioral code in ion-mem; everything
the scaffold prepared for now gets exercised.

Behavior parity with upstream engram on the in-scope subset; new or divergent
behavior is explicitly out of scope for this change.

---

## 2. Why now

| Driver | Detail |
|--------|--------|
| Strategic | The strategic direction (engram observation `architecture/ion-memory-strategy`) defines the local layer as a faithful clone of engram. The store is the heart of that layer — every other local subsystem reads/writes through it. |
| Dependency unblock | `mcp-mvp`, `cloud-mvp`, and `project-detection` all need a working store interface. Without it, those changes have nothing concrete to call. |
| Scaffold ROI | `scaffold-project` (archived 2026-06-06) installed empty `internal/store/doc.go`, CI, build, lint, and test wiring. That investment only pays off once real code lands. |
| TDD activation | This change introduces the first `*_test.go` files in the repo. After verify+archive, `sdd-init` should re-run to flip `strict_tdd: true` in the testing-capabilities cache so every future change defaults to strict TDD. |

---

## 3. In scope

### 3.1 Schema (v1, single migration)

| Table | Purpose |
|-------|---------|
| `sessions` | id, project, directory, started_at, ended_at, summary, status |
| `observations` | id, sync_id, session_id, type, title, content, tool_name, project, scope, topic_key, normalized_hash, revision_count, duplicate_count, last_seen_at, created_at, updated_at, deleted_at |
| `observations_fts` | FTS5 virtual table on title + content + tool_name + type + project |
| `user_prompts` | id, sync_id, session_id, content, project, created_at |
| `prompts_fts` | FTS5 virtual table on content + project |
| `schema_version` | single-row table tracking applied migration version |

Database pragmas: `journal_mode=WAL`, `busy_timeout=5000`, `foreign_keys=ON`,
`synchronous=NORMAL`. FTS5 sync triggers (`INSERT`, `UPDATE`, `DELETE`) keep
the virtual tables coherent with their owning tables.

### 3.2 Operations

| Surface | Functions |
|---------|-----------|
| Lifecycle | `Open(dataDir string) (*Store, error)`, `(*Store).Close() error` |
| Sessions | `CreateSession`, `EndSession`, `GetSession`, `RecentSessions`, `DeleteSession` |
| Observations (write) | `AddObservation` (dedupe by normalized hash + project + scope + type + title, topic-key upsert, `revision_count++`, `duplicate_count++`, `last_seen_at` bump), `UpdateObservation` (by ID), `DeleteObservation` (soft by default, hard with flag) |
| Observations (read) | `GetObservation`, `RecentObservations` (project/scope filtered, soft-delete aware), `Search` (FTS5 BM25 ranked, scoped by project/type/scope), `Timeline` (before/after observations in same session) |
| Prompts | `AddPromptIfMissing` (dedup by session + content hash), `RecentPrompts`, `SearchPrompts`, `DeletePrompt` |
| Reporting | `Stats` (counts per project/type/scope) |

### 3.3 Tests

Black-box tests in `internal/store/store_test.go` (table-driven where useful):

- Schema migration: open empty DB → verify tables, indexes, FTS virtuals, schema_version row
- Open existing DB: idempotent migration, no destructive drift
- Sessions CRUD + ended_at + status filtering
- Observation dedupe: same content twice → second is no-op, `duplicate_count++`, `last_seen_at` bumps
- Topic-key upsert: same `(project, scope, topic_key)` updates in place, `revision_count++`
- Soft-delete filter: deleted rows excluded from `Search`, `RecentObservations`, `Timeline`
- FTS5 BM25 ranking: relevant terms surface higher
- FTS5 tokenization: kebab-case topic keys searchable as fragments
- Foreign-key enforcement: deleting a session with observations returns `ErrSessionHasObservations` (see Open Question #5)
- Prompt dedup: same prompt content in same session is no-op
- Concurrent write retry: `SQLITE_BUSY` triggers the documented backoff and succeeds

### 3.4 Dependencies introduced

- `modernc.org/sqlite` v1.45.x — pure Go SQLite driver (recommended)
- No test assertion library (stdlib `testing` + small helpers) — recommended

This is the first non-empty `require` block in `go.mod`.

---

## 4. Out of scope

| Deferred to | Item |
|-------------|------|
| `mcp-conflict-surfacing` or `cloud-mvp` | `memory_relations` table + conflict surfacing |
| `cloud-mvp` | `sync_mutations`, `sync_apply_deferred`, sync cursors, project-scoped backups |
| Future change | `mem_judge` / `mem_compare` semantics |
| Future change | LLM runners for semantic scan |
| `mcp-mvp` | MCP server protocol |
| `http-server-mvp` | HTTP/REST server |
| `cli-mvp` / `tui-mvp` | CLI + TUI binaries |
| `setup-mvp` | First-run init flow |
| `cloud-mvp` | Cloud sync engine |
| `project-detection` | Project auto-detection algorithm |
| Never | Multi-database backends (SQLite-only product decision) |

---

## 5. Approach

| Decision | Choice |
|----------|--------|
| Driver | `modernc.org/sqlite` (pure Go, matches upstream, no cgo, easy cross-compile) |
| Layout | Concrete `*Store` struct in `internal/store/store.go`. No interface in v1 — extract later if MCP/server tests need fakes. |
| Migrations | Single linear migration function `migrate(db *sql.DB) error` keyed off the `schema_version` table. Engram-style; no `golang-migrate` dependency. |
| Time | TEXT ISO-8601 (RFC3339) in SQLite, matches upstream. A small `internal/timeutil` helper package is acceptable if it stays tiny. |
| Hashing | SHA-256 via stdlib `crypto/sha256` for normalized-content dedupe. Fast enough at our scale; matches upstream. |
| Defaults | `scope=project` default on observations, matches engram semantics. |
| Errors | Sentinel `Err*` values (mirroring engram's `ErrSessionNotFound`, `ErrSessionHasObservations`, `ErrObservationNotFound`, `ErrPromptNotFound`) so callers can use `errors.Is`. |
| Concurrency | SQLite WAL + `busy_timeout=5000ms` + documented backoff retries (3 attempts: 10ms, 25ms, 50ms) on `SQLITE_BUSY` / `SQLITE_LOCKED`. |
| Testing | Standard library `testing`. Each test opens a fresh temp-dir DB via `t.TempDir()`. Black-box: tests live in `package store_test` to keep API discipline. Helpers (e.g. `mustOpen`, `mustAdd`) live in `store_helpers_test.go`. |
| TDD | Strict TDD red → green → refactor for every behavior. This change activates strict TDD for the project (see Why Now). |

The structure is intentionally a behavioral subset of engram's `internal/store`.
We do **not** copy code; we re-implement against the upstream as a reference
shape so future merge-from-upstream is possible but not mandatory.

---

## 6. Capabilities introduced

| Capability | Lands at | After |
|------------|----------|-------|
| `local-store` | `openspec/specs/local-store.md` (Status: Active) | Archive of this change |

The capability spec will define the contract every later subsystem depends on:
the schema shape, the operation surface, dedupe semantics, soft-delete
semantics, search ranking guarantees, and error sentinels.

---

## 7. Open questions

These questions must be resolved during `sdd-spec` / `sdd-design`; each has a
recommended answer noted inline.

1. **SQLite driver**: `modernc.org/sqlite` (pure Go, upstream match) vs `mattn/go-sqlite3` (cgo, mature). **Recommendation**: modernc — matches upstream and keeps cross-compile easy.
2. **Test assertion library**: stdlib `testing` only vs adding `testify/require`. **Recommendation**: stdlib + small helpers — less dep weight, matches upstream.
3. **Schema versioning**: simple `schema_version` table + linear migration funcs (~50 LOC) vs `golang-migrate` library. **Recommendation**: simple table + linear funcs — zero extra deps.
4. **`Store` interface**: define an interface for testability now vs only the concrete `*Store` type. **Recommendation**: concrete `*Store` for v1; extract an interface later if MCP/server tests need fakes.
5. **Session deletion semantics**: return 409-equivalent (`ErrSessionHasObservations`) if observations exist vs cascade-delete observations. **Recommendation**: 409 in v1 (safer); cascade lives in a cmd-level helper later.
6. **Hash function for dedupe**: SHA-256 vs blake3 vs FNV. **Recommendation**: SHA-256 (`crypto/sha256`) — stdlib, matches upstream, plenty fast for this scale.
7. **`scope` default**: default to `project` (engram parity)? **Recommendation**: yes — same default semantics.
8. **Time storage**: TEXT ISO-8601 (engram parity) vs INTEGER unix seconds. **Recommendation**: TEXT ISO-8601 to match upstream.

---

## 8. Acceptance criteria

- `go build ./...` exits 0.
- `go test ./...` exits 0 with at least 1 passing test in `internal/store`.
- `gofmt -l .` produces no output.
- `go vet ./...` exits 0.
- `internal/store/store.go` exports `Store`, `Open`, and the operation surface listed in §3.2.
- Sentinel errors documented in §5 are exported and used by the relevant operations.
- Schema migration succeeds against an empty data dir and is idempotent against an existing one.
- Tests cover at minimum: schema setup, sessions CRUD, dedupe, topic-key upsert, soft-delete filtering, FTS5 ranking, foreign-key enforcement, prompt dedup, busy retry.
- No new direct dependencies beyond the chosen SQLite driver.
- `go.mod` `require` block lists exactly the SQLite driver (transitive deps are fine).
- Strict TDD discipline followed throughout `sdd-apply`; every behavior introduced via red → green → refactor.

---

## 9. Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Pure-Go SQLite (`modernc`) perf gap vs cgo `mattn` | Slower writes at high concurrency | Acceptable at MVP scale; revisit if benchmarks show a problem post-MVP. |
| FTS5 tokenizer behavior surprises on kebab-case / dotted identifiers | Search misses on `topic_key`-style strings | Explicit tokenization tests in §3.3. Use upstream's tokenization config (`unicode61 remove_diacritics 2`) as starting point. |
| Schema drift from engram upstream over time | Future upstream merges become painful | Document divergences in the `local-store` capability spec; treat upstream as reference, not source. |
| Soft-delete edge cases (deleted rows leaking through one query path) | Stale data surfaces to callers | Cover every read path with a "deleted row excluded" test in §3.3. |
| Strict TDD ramp cost for the first behavioral change | Slower apply phase than expected | Accept the cost — establishing the discipline now pays compounding interest. |
| Migration complexity creep | Single migration grows into many before we ship anything | Keep v1 schema in ONE migration. Later schema changes get their own SDD changes with explicit migrations. |

---

## 10. Rollback plan

- This change is purely additive to `internal/store` and `go.mod`. No other subsystem depends on it yet (MCP, server, sync, etc. are still empty `doc.go` packages from `scaffold-project`).
- Rollback = revert the change PR. Nothing in production depends on the store. CI returns to passing trivially because the scaffold's `go test ./...` over empty packages still exits 0.
- No database compatibility concerns: no user data exists yet for ion-mem.

---

## 11. Dependencies

| Type | Item |
|------|------|
| Required (archived) | `scaffold-project` — provides `go.mod`, `internal/store/doc.go` placeholder, CI, Makefile, lint/format. |
| Blocks | `mcp-mvp`, `cloud-mvp`, `project-detection`, `cli-mvp`, `tui-mvp` — all read/write through the store. |
| Parallel-safe | None at this time. This is the next critical-path change. |

---

## 12. Strict TDD activation note

This is the change where Strict TDD turns on for `ion-mem`.

- `sdd-apply` for this change MUST follow red → green → refactor for every behavior in §3.3.
- After verify + archive, the orchestrator should re-run `sdd-init` so the testing-capabilities cache flips `strict_tdd: true`.
- Downstream phases (`sdd-spec`, `sdd-design`, `sdd-tasks`, `sdd-apply`) must treat strict TDD as a non-negotiable for this change's implementation phase.
