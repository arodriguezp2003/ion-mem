---
ion-sdd-version: "1.0"
phase: ion-sdd-tasks
generated: "2026-06-11T00:00:00Z"
mode: openspec
change: "cloud-data-model"
strict_tdd: false
delivery_strategy: "ask-on-risk"
chain_strategy: "stacked-to-main"
---

# Tasks: Cloud Data Model — Postgres Foundation

## Review Workload Forecast

| Field | Value |
|---|---|
| Estimated changed lines | PR-1: ~480–560 lines / PR-2: ~280–340 lines |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | PR-1 (ion-mem-types + scaffold + migrations 0001–0005 + sqlc + tests) → PR-2 (migrations 0006–0007 + sqlc + tests) |
| Delivery strategy | ask-on-risk |
| Chain strategy | stacked-to-main |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: stacked-to-main
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Notes |
|---|---|---|---|
| 1 | ion-mem-types module: DTOs + unit tests | PR-1 → main | No DB. Fully independent; compiles standalone. Tests included. |
| 2 | ion-mem-cloud scaffold: go.mod, migration 0001 (pgvector ext + enum), internal/migrate, internal/testdb | PR-1 → main | Foundation for all migrations. Requires Docker for testcontainers tests. |
| 3 | Migrations 0002–0005 (users/projects/members/invites) + sqlc queries + schema integration tests | PR-1 → main | Depends on unit 2. Tests gated by testing.Short(). |
| 4 | Migrations 0006–0007 (observations + audit_log) + sqlc queries + integration tests | PR-2 → main | Depends on PR-1 merged. Tests gated by testing.Short(). |

---

## Phase 1: ion-mem-types Module (PR-1)

- [x] 1.1 Create `ion-mem-types/go.mod` with module `github.com/ionix/ion-mem-types`; no external dependencies.
- [x] 1.2 Create `ion-mem-types/types.go` exporting `Role` (`owner`/`editor`/`viewer`), `User`, `Project`, `Member`, `Invite`, `Observation` wire DTOs — no DB tags, no storage imports.
- [x] 1.3 Write `ion-mem-types/types_test.go` with table-driven unit tests: JSON round-trip for each DTO; `Role` marshals/unmarshals the three enum strings exactly. Verify: `go test ./...` passes with no DB.

## Phase 2: ion-mem-cloud Scaffold (PR-1)

- [x] 2.1 Create `go.mod` for module `github.com/ionix/ion-mem-cloud`; add `pgx/v5`, `golang-migrate`, `testcontainers-go`, `google/uuid`, `sqlc` tool dependency.
- [x] 2.2 Create `migrations/0001_init.up.sql`: `CREATE EXTENSION IF NOT EXISTS vector;` and `CREATE TYPE project_role AS ENUM ('owner','editor','viewer');`.
- [x] 2.3 Create `migrations/0001_init.down.sql`: drop `project_role` enum; drop pgvector extension.
- [x] 2.4 Create `internal/migrate/migrate.go`: embed `migrations/` FS; expose `Up(db)` and `Down(db)` using golang-migrate/pgx driver.
- [x] 2.5 Create `internal/testdb/container.go`: start ephemeral Postgres via testcontainers-go; return `*pgxpool.Pool` and a `Cleanup()` func; respect `testing.Short()` skip.
- [x] 2.6 Create `cmd/migrate/main.go`: CLI that calls `migrate.Up` or `migrate.Down` from `os.Args[1]`.
- [x] 2.7 Write `internal/migrate/migrate_test.go`: integration test — apply up on fresh container, assert `project_role` enum exists via `information_schema`; apply down, assert enum gone. Gated by `testing.Short()`.

## Phase 3: Migrations 0002–0005 + sqlc + Queries (PR-1)

- [x] 3.1 Create `migrations/0002_users.up.sql` per design DDL: `users` table + `users_email_key` partial unique index.
- [x] 3.2 Create `migrations/0002_users.down.sql`: drop index then drop `users` table.
- [x] 3.3 Create `migrations/0003_projects.up.sql`: `projects` table + `projects_slug_key` partial unique index.
- [x] 3.4 Create `migrations/0003_projects.down.sql`: drop index then drop `projects`.
- [x] 3.5 Create `migrations/0004_project_members.up.sql`: `project_members` table + `members_project_user_key` partial unique index.
- [x] 3.6 Create `migrations/0004_project_members.down.sql`: drop index then drop `project_members`.
- [x] 3.7 Create `migrations/0005_invites.up.sql`: `invites` table + `invites_project_idx` index.
- [x] 3.8 Create `migrations/0005_invites.down.sql`: drop index then drop `invites`.
- [x] 3.9 Create `sqlc.yaml`: engine `pgx/v5`; `vector` custom type override → `[]byte`; output to `internal/db/`.
- [x] 3.10 Create `query/users.sql`, `query/projects.sql`, `query/project_members.sql`, `query/invites.sql` with named sqlc queries (insert, get-by-id, soft-delete, list-active).
- [x] 3.11 Run `sqlc generate`; commit the generated `internal/db/` package.

## Phase 4: Schema Integration Tests — PR-1 Tables

- [x] 4.1 Write `internal/db/users_test.go`: insert user with uuid v7 id; assert uuid v7 time-orderedness. Insert duplicate email (active) — assert unique constraint error (spec: User Identity Shape). Gated by `testing.Short()`.
- [x] 4.2 Write `internal/db/project_members_test.go`: insert orphan member with non-existent `project_id` — assert FK error (spec: FK Integrity). Insert duplicate active membership — assert partial unique constraint error (spec: Role Uniqueness). Soft-delete row then re-insert same `(project_id, user_id)` — assert success (spec: Re-invite after soft-delete). Gated by `testing.Short()`.
- [x] 4.3 Write `internal/db/invites_test.go`: insert invite with invalid role value — assert enum constraint error (spec: Invalid role rejected). Insert two invites with same token — assert unique constraint error (spec: Token uniqueness enforced). Gated by `testing.Short()`.
- [x] 4.4 Write `internal/migrate/roundtrip_test.go`: apply all up migrations then all down migrations on fresh container; assert no error and schema returns to baseline (spec: Migration Reversibility). Gated by `testing.Short()`.

## Phase 5: Migrations 0006–0007 + sqlc + Queries (PR-2)

- [x] 5.1 Create `migrations/0006_observations.up.sql`: `observations` table + `obs_topic_lww_key`, `obs_dedupe_idx`, `obs_project_idx` indexes per design DDL; `embedding vector` column nullable, unconstrained.
- [x] 5.2 Create `migrations/0006_observations.down.sql`: drop indexes then drop `observations`.
- [x] 5.3 Create `migrations/0007_audit_log.up.sql`: `audit_log` table + `audit_project_idx` index.
- [x] 5.4 Create `migrations/0007_audit_log.down.sql`: drop index then drop `audit_log`.
- [x] 5.5 Create `query/observations.sql` and `query/audit_log.sql` with named sqlc queries (upsert/insert, get-by-sync-id, list-by-project, append-audit-entry).
- [x] 5.6 Re-run `sqlc generate`; commit updated `internal/db/` with observation and audit_log generated code.

## Phase 6: Schema Integration Tests — PR-2 Tables

- [x] 6.1 Write `internal/db/observations_test.go`: insert observation with NULL `author_id` — assert success (spec: Nullable FK allows NULL). Insert observation with NULL `embedding` — assert no error (spec: Vector column present but unpopulated). Insert duplicate `sync_id` — assert unique constraint error. Insert duplicate active `(project_id, scope, topic_key)` — assert partial unique error. Gated by `testing.Short()`.
- [x] 6.2 Write `internal/db/audit_log_test.go`: insert audit entry with NULL `actor_id` — assert success (spec: System action with no actor). Assert no UPDATE/DELETE SQL queries exist in `query/audit_log.sql` (spec: Audit Log Append-Only). Gated by `testing.Short()`.
- [x] 6.3 Update `internal/migrate/roundtrip_test.go` to include migrations 0006–0007 in the full up-then-down roundtrip assertion.
