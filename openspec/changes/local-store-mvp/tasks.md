# Tasks: local-store-mvp

**Change**: `local-store-mvp`
**Package**: `internal/store`
**Strict TDD**: ACTIVE — red → green → refactor on every behavior task
**Delivery**: 3 stacked-to-main PRs (Slice 1 → 2 → 3)

---

## Part 1: Review Workload Forecast

| Field | Value |
|---|---|
| Total estimated lines | ~1525 (475 + 625 + 425) |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Chain strategy | stacked-to-main |
| Delivery strategy | auto-chain |
| Decision needed before apply | No (cached: 3 PRs stacked-to-main) |

Decision needed before apply: No
Chained PRs recommended: Yes
Chain strategy: stacked-to-main
400-line budget risk: High

---

## Part 2: Suggested Work Units

| Unit | PR Title | Slice | Scope | Approx. Lines |
|------|----------|-------|-------|---------------|
| 1 | `feat(store): slice 1 — schema + sessions` | Slice 1 | store.go, migrations.go, errors.go, helpers.go, schema_0001_sessions.go, sessions.go + 3 test files | ~475 |
| 2 | `feat(store): slice 2 — observations + FTS5 + search` | Slice 2 | schema_0002_observations.go, observations.go, observations_test.go, search_test.go | ~625 |
| 3 | `feat(store): slice 3 — prompts + timeline + stats` | Slice 3 | schema_0003_prompts.go, prompts.go, timeline.go, prompts_test.go, timeline_test.go | ~425 |

---

## Part 3: Slice 1 — Schema + Sessions

PR: `feat(store): slice 1 — schema + sessions` → base: `main`

- [x] `[PREP] 1.1` Add `modernc.org/sqlite v1.45.0` to `go.mod`, run `go mod tidy`, verify it is the only direct non-empty dep. (S1-R01)
- [x] `[PREP] 1.2` Create `internal/store/errors.go` — export `ErrNotFound`, `ErrSessionHasObservations`, `ErrObservationNotFound`, `ErrPromptNotFound`, `ErrMigrationFailed` as `var` sentinels using `errors.New`. (S1-R17)
- [x] `[PREP] 1.3` Create `internal/store/migrations.go` — `schema_version` DDL + `migrate(db *sql.DB) error` runner: creates table, reads applied versions, calls each registered migration func in order, records rows via `INSERT OR IGNORE`. No migrations registered yet. (S1-R06)
- [x] `[PREP] 1.4` Create `internal/store/helpers.go` — `nowISO() string`, `parseISO(s string) (time.Time, error)`, `normalizeScope(s string) string`, `sanitizeFTS(query string) string` (wrap each term in double quotes). (CC-R08, S2-R18)
- [x] `[TDD-RED] 1.5` Write failing test `TestOpen_CreatesSchemaVersionRow` in `internal/store/store_test.go` (calls `mustOpen` — stub helper that will fail until Open + SchemaVersion exist). (S1-T01)
- [x] `[TDD-RED] 1.6` Write `mustOpen(t *testing.T) *store.Store` stub in `internal/store/store_helpers_test.go` using `t.TempDir()` and `t.Cleanup`. (CC-R07)
- [x] `[TDD-GREEN] 1.7` Create `internal/store/store.go` — `Store` struct with `db *sql.DB`; `Open(dataDir string) (*Store, error)` validating absolute path + file-not-dir, creating dir, `sql.Open`, `SetMaxOpenConns(1)`, applying pragmas in order (WAL, busy_timeout, foreign_keys, synchronous), calling `migrate`; `(*Store).Close() error`; `SchemaVersion() int` accessor. (S1-R02..R05, S1-R18, S1-R19)
- [x] `[TDD-RED] 1.8` Write failing tests: `TestOpen_IsIdempotent` (S1-T02), `TestOpen_RejectsRegularFilePath` (S1-T03), `TestOpen_RejectsRelativePath` (S1-T04). (S1-R02, S1-R18, S1-R19)
- [x] `[TDD-GREEN] 1.9` Fix `Open` to handle the idempotency, file-exists, and relative-path guards (if not already passing from 1.7). Confirm all 4 `store_test.go` tests pass.
- [x] `[TDD-RED] 1.10` Write failing tests: `TestOpen_WALEnabled` (S1-T05), `TestOpen_BusyTimeout` (S1-T06), `TestOpen_ForeignKeysOn` (S1-T07). (S1-R04)
- [x] `[TDD-GREEN] 1.11` Confirm pragma tests pass (should pass from 1.7 implementation). If not, fix pragma application order in `Open`.
- [x] `[PREP] 1.12` Create `internal/store/schema_0001_sessions.go` — DDL constant + `applyMigration0001(db *sql.DB) error` function (`sessions` table + 2 indexes). Register it in `migrations.go`. (S1-R07, S1-R08)
- [x] `[TDD-RED] 1.13` Write failing test `TestMigration0001_CreatesSessionsTable` in `store_test.go` — queries `sqlite_master` for `sessions` table and both indexes. (S1-T01)
- [x] `[TDD-GREEN] 1.14` Confirm test passes once `schema_0001_sessions.go` is registered. Fix registration if needed.
- [x] `[TDD-RED] 1.15` Write failing tests: `TestCreateSession_RoundTrip` (S1-T08). (S1-R09, S1-R14)
- [x] `[TDD-GREEN] 1.16` Create `internal/store/sessions.go` — `CreateSessionParams`, `Session` struct, `CreateSession(ctx, params) (Session, error)`. (S1-R09, CC-R09)
- [x] `[TDD-RED] 1.17` Write failing test `TestCreateSession_DuplicateIDReturnsError` (S1-T09). (S1-R10)
- [x] `[TDD-GREEN] 1.18` Confirm PK conflict surfaces non-nil error from `CreateSession`. Wrap driver error if needed.
- [x] `[TDD-RED] 1.19` Write failing tests: `TestGetSession_NotFoundReturnsSentinel`. (S1-R14)
- [x] `[TDD-GREEN] 1.20` Implement `GetSession(ctx, sessionID string) (Session, error)` — returns `ErrNotFound` on `sql.ErrNoRows`. (S1-R14)
- [x] `[TDD-RED] 1.21` Write failing tests: `TestEndSession_SetsEndedAtAndStatus` (S1-T10), `TestEndSession_NotFoundReturnsSentinel` (S1-T11). (S1-R11..R13)
- [x] `[TDD-GREEN] 1.22` Implement `EndSession(ctx, sessionID, summary string) error` — idempotent UPDATE; returns `ErrNotFound` on 0 rows affected. (S1-R11..R13)
- [x] `[TDD-RED] 1.23` Write failing tests: `TestRecentSessions_OrderingAndLimit` (S1-T12), `TestRecentSessions_AllProjectsWhenEmpty` (S1-T13). (S1-R15)
- [x] `[TDD-GREEN] 1.24` Implement `RecentSessions(ctx, project string, limit int) ([]Session, error)` — default limit 50, `started_at DESC` order, optional project filter. (S1-R15)
- [x] `[TDD-RED] 1.25` Write failing tests: `TestDeleteSession_Succeeds` (S1-T14), `TestDeleteSession_NotFoundReturnsSentinel` (S1-T16). (S1-R16)
- [x] `[TDD-GREEN] 1.26` Implement `DeleteSession(ctx, sessionID string) error` — checks row exists first (→ `ErrNotFound`), then `DELETE`. FK constraint for `ErrSessionHasObservations` is enforced at DB level and will surface correctly once observations exist in Slice 2; for Slice 1, no FK children exist so delete succeeds. (S1-R16)
- [x] `[TDD-REFACTOR] 1.27` Extract `mustSession(t, s, project string) store.Session` helper to `store_helpers_test.go`. Reduce duplication in `sessions_test.go`. All tests must remain green.
- [x] `[VERIFY] 1.28` Run `go build ./...`, `go test ./internal/store/...`, `go vet ./...`, `gofmt -l .` — all exit 0, no skipped tests, ≥12 distinct test cases.
- [ ] `[COMMIT] 1.29` Work-unit commit: `feat(store): slice 1 — schema + sessions`
- [ ] `[PR] 1.30` Open PR #1 targeting `main`. Wait for CI green + merge before starting Slice 2.

---

## Part 4: Slice 2 — Observations + FTS5 + Search

PR: `feat(store): slice 2 — observations + FTS5 + search` → base: `main` (rebase after PR 1 merges)

- [ ] `[PREP] 2.1` Create `internal/store/schema_0002_observations.go` — DDL constant with `observations` table + 8 indexes + `observations_fts` virtual table + 3 FTS5 sync triggers + `applyMigration0002(db)`. Register in `migrations.go`. (S2-R01..R04, S2-R19)
- [ ] `[TDD-RED] 2.2` Write failing test `TestMigration0002_AppliesOnTopOf0001` (S2-T01) — queries `sqlite_master` for `observations`, `observations_fts`, verifies `schema_version` has rows 1 and 2. (S2-R19)
- [ ] `[TDD-GREEN] 2.3` Confirm test passes. Fix DDL registration if needed.
- [ ] `[PREP] 2.4` Add `normalizedHash(content string) string` and `generateSyncID(prefix string) string` to `internal/store/helpers.go`. (S2-R06, S2-R18)
- [ ] `[TDD-RED] 2.5` Write failing test `TestAddObservation_NewRow` (S2-T02) — insert + GetObservation round-trip, verify `RevisionCount=1`, `DuplicateCount=0`, `sync_id` prefix. (S2-R05, S2-R09)
- [ ] `[TDD-GREEN] 2.6` Create `internal/store/observations.go` — `AddObservationParams`, `Observation` struct, `AddObservation` new-row path only. (S2-R05, S2-R09, CC-R09)
- [ ] `[TDD-RED] 2.7` Write failing test `TestAddObservation_Deduplication` (S2-T03) — same `(hash, project, scope, type, title)` increments `DuplicateCount`, updates `LastSeenAt`. (S2-R07)
- [ ] `[TDD-GREEN] 2.8` Implement dedup probe in `AddObservation` (query `idx_obs_dedupe`, UPDATE if match, return existing row). (S2-R07)
- [ ] `[TDD-RED] 2.9` Write failing tests: `TestAddObservation_TopicKeyUpsert_IncrementsRevision` (S2-T04), `TestAddObservation_TopicKeyUpsert_NoExistingRowInsertsNew` (S2-T05). (S2-R08)
- [ ] `[TDD-GREEN] 2.10` Implement topic-key upsert in `AddObservation` (checked before dedup): query `idx_obs_topic`, UPDATE if match, fall through to new-row otherwise. (S2-R08)
- [ ] `[TDD-RED] 2.11` Write failing test `TestAddObservation_RejectsUnknownSession` (S2-T06). (S2-R10)
- [ ] `[TDD-GREEN] 2.12` Confirm FK driver error surfaces as non-nil error. Wrap if needed.
- [ ] `[TDD-RED] 2.13` Write failing tests: `TestGetObservation_RoundTrip`, `TestGetObservation_NotFound` (S2-T18). (S2-R12)
- [ ] `[TDD-GREEN] 2.14` Implement `GetObservation(ctx, id int64) (Observation, error)` — excludes soft-deleted, returns `ErrObservationNotFound`. (S2-R12)
- [ ] `[TDD-RED] 2.15` Write failing test `TestUpdateObservation_PartialUpdate` (S2-T17). (S2-R11)
- [ ] `[TDD-GREEN] 2.16` Implement `UpdateObservationParams`, `UpdateObservation(ctx, id, params)` — nil fields skipped, `updated_at` always set. (S2-R11)
- [ ] `[TDD-RED] 2.17` Write failing tests: `TestRecentObservations_ExcludesSoftDeleted` (partial S2-T07), `TestRecentObservations_ProjectAndScopeFilters`. (S2-R14)
- [ ] `[TDD-GREEN] 2.18` Implement `RecentObservationsParams`, `RecentObservations` — filters `deleted_at IS NULL`, `created_at DESC`, default limit 50. (S2-R14)
- [ ] `[TDD-RED] 2.19` Write failing tests: `TestDeleteObservation_SoftDelete` (S2-T07), `TestDeleteObservation_HardDelete` (S2-T08), `TestDeleteObservation_NotFound` (S2-T19). (S2-R15)
- [ ] `[TDD-GREEN] 2.20` Implement `DeleteObservation(ctx, id int64, hard bool) error` — soft: SET `deleted_at`; hard: DELETE row + FTS. (S2-R15)
- [ ] `[TDD-RED] 2.21` Write failing tests: `TestSearch_BM25Ranking` (S2-T09), `TestSearch_TypeFilter` (S2-T10), `TestSearch_ProjectFilter` (S2-T11), `TestSearch_ScopeFilter` (S2-T12), `TestSearch_EmptyResult` (S2-T15), `TestSearch_ExcludesSoftDeleted` (S2-T16). (S2-R16, S2-R17)
- [ ] `[TDD-GREEN] 2.22` Implement `SearchParams`, `SearchResult`, `Search(ctx, params)` — FTS5 BM25 JOIN, `sanitizeFTS`, optional filters, `deleted_at IS NULL` guard. (S2-R16, S2-R17)
- [ ] `[TDD-RED] 2.23` Write failing FTS5 tokenization tests: `TestSearch_FTS5KebabFragment` (S2-T13), `TestSearch_FTS5KebabFull` (S2-T14) in `internal/store/search_test.go`. (S2-R02)
- [ ] `[TDD-GREEN] 2.24` Confirm tokenizer tests pass (FTS5 `unicode61 remove_diacritics 2` should handle kebab via hyphen splitting). Fix sanitizeFTS quoting strategy if needed.
- [ ] `[TDD-RED] 2.25` Write failing test `TestDeleteSession_BlockedByObservations` (S2-T20) — insert observation, attempt `DeleteSession` → `ErrSessionHasObservations`. (S1-R16, S2-R20 backfill)
- [ ] `[TDD-GREEN] 2.26` Confirm FK RESTRICT blocks `DeleteSession` and error is mapped to `ErrSessionHasObservations`. Fix error wrapping in `sessions.go` if needed.
- [ ] `[TDD-RED] 2.27` Write failing concurrency test `TestAddObservation_ConcurrentSuccess` — two goroutines, same store, both must succeed. (S2-R20)
- [ ] `[TDD-GREEN] 2.28` Confirm WAL + busy_timeout handles concurrency. Adjust `busy_timeout` value if test is flaky.
- [ ] `[TDD-REFACTOR] 2.29` Extract `mustObservation(t, s, sessionID string) store.Observation` helper to `store_helpers_test.go`. Reduce duplication across `observations_test.go` and `search_test.go`.
- [ ] `[VERIFY] 2.30` Run `go build ./...`, `go test ./internal/store/...`, `go vet ./...`, `gofmt -l .` — all exit 0, no skipped tests, ≥20 new distinct test cases, Slice 1 tests still pass.
- [ ] `[COMMIT] 2.31` Work-unit commit: `feat(store): slice 2 — observations + FTS5 + search`
- [ ] `[PR] 2.32` Rebase on `main` (after PR 1 merges). Open PR #2 targeting `main`. Wait for CI green + merge before starting Slice 3.

---

## Part 5: Slice 3 — Prompts + Timeline + Stats

PR: `feat(store): slice 3 — prompts + timeline + stats` → base: `main` (rebase after PR 2 merges)

- [ ] `[PREP] 3.1` Create `internal/store/schema_0003_prompts.go` — DDL constant with `user_prompts` table + 3 indexes + `prompts_fts` virtual table + 3 FTS5 sync triggers + `applyMigration0003(db)`. Register in `migrations.go`. (S3-R01..R04, S3-R15)
- [ ] `[TDD-RED] 3.2` Write failing test `TestMigration0003_AppliesOnTopOf0001And0002` (S3-T01) — queries `sqlite_master`, checks `schema_version` has rows 1, 2, 3. (S3-R15)
- [ ] `[TDD-GREEN] 3.3` Confirm test passes. Fix registration if needed.
- [ ] `[TDD-RED] 3.4` Write failing tests: `TestAddPromptIfMissing_InsertsNew` (S3-T02). (S3-R05)
- [ ] `[TDD-GREEN] 3.5` Create `internal/store/prompts.go` — `AddPromptParams`, `Prompt` struct, `AddPromptIfMissing(ctx, params)` insert path; `sync_id` prefix `"pr-"`; dedup by SHA-256 of `(content + session_id)`. (S3-R05, CC-R09)
- [ ] `[TDD-RED] 3.6` Write failing tests: `TestAddPromptIfMissing_DedupesSameSessionAndContent` (S3-T03), `TestAddPromptIfMissing_AllowsSameContentDifferentSession` (S3-T04). (S3-R05)
- [ ] `[TDD-GREEN] 3.7` Implement dedup probe in `AddPromptIfMissing` — SELECT on `(session_id, content hash)` before INSERT. Return existing row if found. (S3-R05)
- [ ] `[TDD-RED] 3.8` Write failing test `TestAddPromptIfMissing_RejectsUnknownSession` (S3-T05). (S3-R06)
- [ ] `[TDD-GREEN] 3.9` Confirm FK error surfaces. Wrap if needed.
- [ ] `[TDD-RED] 3.10` Write failing tests: `TestRecentPrompts_OrderingAndLimit` (S3-T06), `TestRecentPrompts_AllProjectsWhenEmpty` (S3-T07). (S3-R07)
- [ ] `[TDD-GREEN] 3.11` Implement `RecentPrompts(ctx, project string, limit int) ([]Prompt, error)` — `created_at DESC`, optional project filter, default limit 50. (S3-R07)
- [ ] `[TDD-RED] 3.12` Write failing tests: `TestSearchPrompts_FTS5Match` (S3-T08), `TestSearchPrompts_EmptyResult` (S3-T09). (S3-R08)
- [ ] `[TDD-GREEN] 3.13` Implement `SearchPromptsParams`, `SearchPrompts(ctx, params)` — FTS5 BM25 on `prompts_fts`, sanitize query, optional project filter, default limit 20. (S3-R08)
- [ ] `[TDD-RED] 3.14` Write failing tests: `TestDeletePrompt_Succeeds` (S3-T10), `TestDeletePrompt_NotFound` (S3-T11). (S3-R09)
- [ ] `[TDD-GREEN] 3.15` Implement `DeletePrompt(ctx, id int64) error` — hard delete only; returns `ErrPromptNotFound` on 0 rows. FTS entry removed via trigger. (S3-R09)
- [ ] `[TDD-RED] 3.16` Write failing test `TestDeleteSession_BlockedByPrompt` — verify `ErrSessionHasObservations` returned when prompt FK-references the session. (S3-R16)
- [ ] `[TDD-GREEN] 3.17` Confirm FK RESTRICT on `user_prompts.session_id` blocks `DeleteSession`. Fix error mapping in `sessions.go` if needed. (S3-R16)
- [ ] `[PREP] 3.18` Create `internal/store/timeline.go` — `TimelineEntry`, `Stats`, `ProjectStats` struct definitions. (S3-R11, S3-R14, CC-R09)
- [ ] `[TDD-RED] 3.19` Write failing tests: `TestTimeline_MiddleAnchor_BeforeAndAfter` (S3-T12), `TestTimeline_StartAnchor_EmptyBefore` (S3-T13). (S3-R10..R12)
- [ ] `[TDD-GREEN] 3.20` Implement `Timeline(ctx, observationID int64, before, after int) ([]TimelineEntry, error)` — queries anchor session + created_at, UNION observations + prompts in same session, exclude soft-deleted obs, sort chronologically, slice window. (S3-R10..R12)
- [ ] `[TDD-RED] 3.21` Write failing tests: `TestTimeline_MixedObservationsAndPrompts` (S3-T14), `TestTimeline_ExcludesSoftDeleted` (S3-T15), `TestTimeline_MissingAnchorReturnsErr` (S3-T16). (S3-R12..R13)
- [ ] `[TDD-GREEN] 3.22` Extend `Timeline` implementation to handle mixed entries and soft-delete exclusion. Confirm `ErrObservationNotFound` on missing anchor. (S3-R12..R13)
- [ ] `[TDD-RED] 3.23` Write failing test `TestStats_AccurateCounts` (S3-T17) — seed 2 sessions, 5 non-deleted + 1 soft-deleted obs, 3 prompts; verify all Stats fields. (S3-R14)
- [ ] `[TDD-GREEN] 3.24` Implement `Stats(ctx) (Stats, error)` — aggregate queries: COUNT non-deleted obs, COUNT prompts, COUNT sessions, GROUP BY project. (S3-R14)
- [ ] `[TDD-REFACTOR] 3.25` Extract `mustPrompt(t, s, sessionID string) store.Prompt` to `store_helpers_test.go`. Clean up any repeated session/observation seeding in `prompts_test.go` and `timeline_test.go`.
- [ ] `[VERIFY] 3.26` Run `go build ./...`, `go test ./internal/store/...`, `go vet ./...`, `gofmt -l .` — all exit 0, no skipped tests, ≥17 new test cases, Slice 1 + 2 still pass.
- [ ] `[COMMIT] 3.27` Work-unit commit: `feat(store): slice 3 — prompts + timeline + stats`
- [ ] `[PR] 3.28` Rebase on `main` (after PR 2 merges). Open PR #3 targeting `main`. Wait for CI green + merge.

---

## Part 6: Cross-cutting Verification (post-Slice 3)

- [ ] CC.1 Confirm all 56 requirements (S1-R01..R20, S2-R01..R20, S3-R01..R16, CC-R01..R11 minus 3 out-of-scope) have a corresponding test or implementation evidence link
- [ ] CC.2 Confirm all 53 scenarios (S1-T01..T16, S2-T01..T20, S3-T01..T17) have a named test function
- [ ] CC.3 Run `go test -cover ./internal/store/...` — inspect coverage report (informational, not a gate for MVP)
- [ ] CC.4 Final clean pass: `go vet ./...`, `gofmt -l .`, `go build ./...`, `go test ./internal/store/...` all exit 0

---

## Part 7: Verification Strategy

| Acceptance Criterion | Verification Step | Slice |
|---|---|---|
| `go.mod` has only `modernc.org/sqlite` direct dep (S1-R01) | `go mod tidy` + `cat go.mod` — one direct `require` entry | Slice 1 |
| `Open` creates dir + applies migration idempotently (S1-R02, S1-R06) | `TestOpen_CreatesSchemaVersionRow`, `TestOpen_IsIdempotent` | Slice 1 |
| Pragmas applied in correct order (S1-R04) | `TestOpen_WALEnabled`, `TestOpen_BusyTimeout`, `TestOpen_ForeignKeysOn` | Slice 1 |
| Session CRUD + sentinel errors (S1-R09..R16) | `sessions_test.go` full suite | Slice 1 |
| Slice 1 acceptance gate | `go build/test/vet/fmt` all exit 0, ≥12 test cases | Slice 1 |
| Migration 0002 applies on 0001 DB (S2-R19) | `TestMigration0002_AppliesOnTopOf0001` | Slice 2 |
| Dedup / topic-key upsert (S2-R07, S2-R08) | `TestAddObservation_Deduplication`, `TestAddObservation_TopicKeyUpsert_*` | Slice 2 |
| FTS5 tokenization on kebab-case (S2-R02) | `TestSearch_FTS5KebabFragment`, `TestSearch_FTS5KebabFull` | Slice 2 |
| Soft-delete excluded from all reads (S2-R12, S2-R14, S2-R16) | `TestRecentObservations_ExcludesSoftDeleted`, `TestSearch_ExcludesSoftDeleted`, `TestDeleteObservation_SoftDelete` | Slice 2 |
| `ErrSessionHasObservations` on delete with children (S1-R16) | `TestDeleteSession_BlockedByObservations` | Slice 2 |
| Slice 2 acceptance gate | All Slice 1 tests pass + ≥20 new cases, FTS5 tokenization tests present | Slice 2 |
| Migration 0003 applies (S3-R15) | `TestMigration0003_AppliesOnTopOf0001And0002` | Slice 3 |
| Prompt dedup (S3-R05) | `TestAddPromptIfMissing_Dedup*` | Slice 3 |
| Timeline edge cases (S3-R10..R13) | `TestTimeline_StartAnchor_EmptyBefore`, `TestTimeline_MixedObservationsAndPrompts`, `TestTimeline_ExcludesSoftDeleted`, `TestTimeline_MissingAnchorReturnsErr` | Slice 3 |
| Stats aggregate accuracy (S3-R14) | `TestStats_AccurateCounts` | Slice 3 |
| Slice 3 acceptance gate | All Slice 1+2 tests pass + ≥17 new cases, timeline edge cases covered | Slice 3 |
