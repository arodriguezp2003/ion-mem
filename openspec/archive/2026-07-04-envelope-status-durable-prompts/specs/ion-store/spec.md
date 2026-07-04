---
ion-sdd-version: "1.0"
phase: ion-sdd-spec
generated: "2026-07-04T00:00:00Z"
mode: openspec
change: "envelope-status-durable-prompts"
capability: "ion-store"
spec_kind: delta
---

# Delta for ion-store

## ADDED Requirements

### Requirement: R-PROMPT-01 — Migration 0004: consumed_at column

Migration 0004 MUST add a nullable `consumed_at TEXT` column to `user_prompts` with a `DEFAULT NULL`. The migration MUST apply cleanly on top of a database that has migrations 0001–0003 applied. Historical rows (all `NULL consumed_at`) are treated as unconsumed — no backfill is required.

#### Scenario: Migration 0004 applies cleanly

- GIVEN a store with migrations 0001, 0002, 0003 applied
- WHEN `store.Open` is called after migration 0004 code is in place
- THEN `schema_version` contains rows for versions 1, 2, 3, and 4
- AND `PRAGMA table_info(user_prompts)` includes `consumed_at` as a nullable TEXT column

#### Scenario: Historical rows are NULL by default

- GIVEN prompt rows inserted before migration 0004 (no `consumed_at` column at insert time)
- WHEN migration 0004 is applied
- THEN all pre-existing rows have `consumed_at = NULL`
- AND the migration MUST NOT fail on a non-empty table

---

### Requirement: R-PROMPT-02 — ConsumeLatestPrompt atomic fetch-and-consume

`ConsumeLatestPrompt(ctx context.Context, sessionID string) (Prompt, bool, error)` MUST exist on `*Store`. Within a single transaction it MUST: (1) SELECT the `user_prompts` row with `consumed_at IS NULL` for the given `session_id`, ordered by `created_at DESC LIMIT 1`; (2) if found, UPDATE that row's `consumed_at` to the current UTC time and return the row with `found = true`; (3) if not found, return the zero `Prompt` value with `found = false` and `err = nil`.

#### Scenario: Unconsumed prompt is found and marked consumed

- GIVEN a `user_prompts` row with `session_id=S`, `consumed_at IS NULL`
- WHEN `ConsumeLatestPrompt(ctx, S)` is called
- THEN the returned `found` is `true`
- AND the returned `Prompt.ID` matches the row
- AND a direct SQL query confirms `consumed_at IS NOT NULL` on that row

#### Scenario: No unconsumed prompt returns found=false

- GIVEN no `user_prompts` row with `consumed_at IS NULL` for `session_id=S`
- WHEN `ConsumeLatestPrompt(ctx, S)` is called
- THEN `found` is `false` and `err` is `nil`

#### Scenario: Atomicity — concurrent calls consume exactly once

- GIVEN one `user_prompts` row with `consumed_at IS NULL` for `session_id=S`
- WHEN two concurrent goroutines call `ConsumeLatestPrompt(ctx, S)` simultaneously
- THEN exactly one returns `found=true`
- AND the other returns `found=false`
- AND `consumed_at` on the row is set exactly once

#### Scenario: Latest unconsumed is selected when multiple exist

- GIVEN two rows for `session_id=S` both with `consumed_at IS NULL`, created at T1 < T2
- WHEN `ConsumeLatestPrompt(ctx, S)` is called
- THEN the row created at T2 is consumed and returned

---

### Requirement: R-PROMPT-03 — Single-consumption guarantee

A `user_prompts` row with `consumed_at IS NOT NULL` MUST NOT be returned by `ConsumeLatestPrompt`. The same row cannot be consumed twice regardless of the number of `ion_save` calls.

#### Scenario: Already-consumed row is skipped

- GIVEN a row with `consumed_at IS NOT NULL` is the only row for session S
- WHEN `ConsumeLatestPrompt(ctx, S)` is called
- THEN `found` is `false`

---

## MODIFIED Requirements

### Requirement: S3-R01 — user_prompts table schema (migration 0003)

Migration 0003 creates `user_prompts` table with columns: `id INTEGER PRIMARY KEY AUTOINCREMENT`, `sync_id TEXT NOT NULL UNIQUE`, `session_id TEXT NOT NULL REFERENCES sessions(id)`, `content TEXT NOT NULL`, `project TEXT NOT NULL`, `created_at TEXT NOT NULL`. Migration 0004 adds `consumed_at TEXT NULL DEFAULT NULL` to this table.

(Previously: `user_prompts` had no `consumed_at` column. Migration 0004 is additive — migration 0003 definition is unchanged.)

#### Scenario: Migration 0003 creates table without consumed_at

- GIVEN a fresh store
- WHEN only migrations 0001–0003 are applied
- THEN `user_prompts` exists but `PRAGMA table_info` does NOT include `consumed_at`

#### Scenario: Migration 0004 adds consumed_at without altering existing rows

- GIVEN a store with migration 0003 applied and existing prompt rows
- WHEN migration 0004 is applied
- THEN the column `consumed_at` is present with `NULL` in all pre-existing rows

---

### Requirement: S3-R05 — AddPromptIfMissing deduplication

`AddPromptIfMissing(ctx context.Context, params AddPromptParams) (Prompt, error)` deduplicates by `(session_id, content)` match (exact content equality, not hash). If an existing row matches the same session and content, return it without inserting — regardless of the existing row's `consumed_at` value. Otherwise INSERT a new row with `consumed_at = NULL`. `sync_id` prefix is `"pr-"`.

(Previously: dedup described as SHA-256 of `(content + session_id)`; the implementation uses direct `(session_id, content)` equality. Spec now matches implementation. The `consumed_at` field is initialized to NULL on insert.)

#### Scenario: Dedup returns existing row regardless of consumed_at

- GIVEN a `user_prompts` row with `session_id=S`, `content=C`, `consumed_at IS NOT NULL`
- WHEN `AddPromptIfMissing` is called with the same session and content
- THEN no new row is inserted
- AND the returned `Prompt` is the existing row (consumed state unaffected)

#### Scenario: New row inserted with consumed_at NULL

- GIVEN no existing row for `(session_id=S, content=C2)`
- WHEN `AddPromptIfMissing` is called
- THEN a new row is inserted with `consumed_at = NULL`
- AND the returned `Prompt` reflects the new row

#### Scenario: Same content in different sessions creates separate rows

- GIVEN content `C` inserted for `session_id=S1`
- WHEN `AddPromptIfMissing` is called with the same content but `session_id=S2`
- THEN a new row is inserted (total prompts increases by 1)
