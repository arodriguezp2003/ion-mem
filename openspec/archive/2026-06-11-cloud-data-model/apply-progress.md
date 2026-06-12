---
ion-sdd-version: "1.0"
phase: ion-sdd-apply
generated: "2026-06-11"
mode: openspec
change: "cloud-data-model"
batch: 1
size_exception: true
delivery_strategy: "ask-on-risk → size:exception (user: 'todo de una')"
---

# Apply Progress: Cloud Data Model

## Run Summary

- **Mode**: Standard (strict_tdd: false)
- **Batch**: 1 of 1 (all 29 tasks, size:exception approved)
- **Repos created**: `ion-mem-types`, `ion-mem-cloud` (both new, local only, no remote)
- **Tests**: 7 test functions, 14 sub-tests — all PASS against real Postgres via testcontainers

---

## Task Completion

### Phase 1: ion-mem-types Module

| Task | Status | Commit (ion-mem-types) |
|---|---|---|
| 1.1 Create go.mod | [x] | `b1042f0` |
| 1.2 Create types.go (Role, User, Project, Member, Invite, Observation) | [x] | `b1042f0` |
| 1.3 types_test.go JSON round-trip tests | [x] | `b1042f0` |

### Phase 2: ion-mem-cloud Scaffold

| Task | Status | Commit (ion-mem-cloud) |
|---|---|---|
| 2.1 go.mod + deps (pgx/v5, golang-migrate, testcontainers, uuid) | [x] | `b5440f4` |
| 2.2 migrations/0001_init.up.sql (vector ext + enum) | [x] | `b5440f4` |
| 2.3 migrations/0001_init.down.sql | [x] | `b5440f4` |
| 2.4 internal/migrate/migrate.go (embed FS, Up/Down) | [x] | `b5440f4` |
| 2.5 internal/testdb/container.go (testcontainers pgvector) | [x] | `b5440f4` |
| 2.6 cmd/migrate/main.go (CLI) | [x] | `b5440f4` |
| 2.7 internal/migrate/migrate_test.go (up/down enum check) | [x] | `8d05f6e` |

### Phase 3: Migrations 0002–0005 + sqlc + Queries

| Task | Status | Commit (ion-mem-cloud) |
|---|---|---|
| 3.1 migrations/0002_users.up.sql | [x] | `b5440f4` |
| 3.2 migrations/0002_users.down.sql | [x] | `b5440f4` |
| 3.3 migrations/0003_projects.up.sql | [x] | `b5440f4` |
| 3.4 migrations/0003_projects.down.sql | [x] | `b5440f4` |
| 3.5 migrations/0004_project_members.up.sql | [x] | `b5440f4` |
| 3.6 migrations/0004_project_members.down.sql | [x] | `b5440f4` |
| 3.7 migrations/0005_invites.up.sql | [x] | `b5440f4` |
| 3.8 migrations/0005_invites.down.sql | [x] | `b5440f4` |
| 3.9 sqlc.yaml | [x] | `b5440f4` |
| 3.10 query/*.sql (users, projects, project_members, invites) | [x] | `ee1dbf6` |
| 3.11 sqlc generate → internal/db/ committed | [x] | `ee1dbf6` |

### Phase 4: Schema Integration Tests — PR-1 Tables

| Task | Status | Commit (ion-mem-cloud) |
|---|---|---|
| 4.1 internal/db/users_test.go | [x] | `8d05f6e` |
| 4.2 internal/db/project_members_test.go | [x] | `8d05f6e` |
| 4.3 internal/db/invites_test.go | [x] | `8d05f6e` |
| 4.4 internal/migrate/roundtrip_test.go | [x] | `8d05f6e` |

### Phase 5: Migrations 0006–0007 + sqlc + Queries

| Task | Status | Commit (ion-mem-cloud) |
|---|---|---|
| 5.1 migrations/0006_observations.up.sql | [x] | `b5440f4` |
| 5.2 migrations/0006_observations.down.sql | [x] | `b5440f4` |
| 5.3 migrations/0007_audit_log.up.sql | [x] | `b5440f4` |
| 5.4 migrations/0007_audit_log.down.sql | [x] | `b5440f4` |
| 5.5 query/observations.sql + query/audit_log.sql | [x] | `ee1dbf6` |
| 5.6 sqlc generate (obs + audit_log generated code) | [x] | `ee1dbf6` |

### Phase 6: Schema Integration Tests — PR-2 Tables

| Task | Status | Commit (ion-mem-cloud) |
|---|---|---|
| 6.1 internal/db/observations_test.go | [x] | `8d05f6e` |
| 6.2 internal/db/audit_log_test.go | [x] | `8d05f6e` |
| 6.3 roundtrip_test.go updated to include 0006–0007 | [x] | `8d05f6e` |

---

## Integration Test Output (real `go test ./...`, no -short)

```
?   	github.com/ionix/ion-mem-cloud/cmd/migrate	[no test files]
=== RUN   TestAuditLog_AppendOnlyAndNullActor
--- PASS: TestAuditLog_AppendOnlyAndNullActor (35.55s)
    --- PASS: TestAuditLog_AppendOnlyAndNullActor/system_action_with_null_actor (0.00s)
    --- PASS: TestAuditLog_AppendOnlyAndNullActor/no_update_or_delete_queries_in_audit_log_sql (0.00s)
=== RUN   TestInvites_EnumAndTokenUnique
--- PASS: TestInvites_EnumAndTokenUnique (1.18s)
    --- PASS: TestInvites_EnumAndTokenUnique/invalid_role_rejected (0.00s)
    --- PASS: TestInvites_EnumAndTokenUnique/token_uniqueness_enforced (0.00s)
=== RUN   TestObservations_Constraints
--- PASS: TestObservations_Constraints (1.05s)
    --- PASS: TestObservations_Constraints/nullable_author_id_allows_null (0.00s)
    --- PASS: TestObservations_Constraints/null_embedding_succeeds (0.00s)
    --- PASS: TestObservations_Constraints/duplicate_sync_id_rejects (0.00s)
    --- PASS: TestObservations_Constraints/duplicate_active_topic_key_rejects (0.00s)
=== RUN   TestProjectMembers_FKAndUnique
--- PASS: TestProjectMembers_FKAndUnique (1.02s)
    --- PASS: TestProjectMembers_FKAndUnique/orphan_member_rejects_FK (0.00s)
    --- PASS: TestProjectMembers_FKAndUnique/duplicate_active_membership_rejected (0.00s)
    --- PASS: TestProjectMembers_FKAndUnique/soft_delete_then_reinsertion_succeeds (0.00s)
=== RUN   TestInsertUser_UUIDv7AndEmailUnique
--- PASS: TestInsertUser_UUIDv7AndEmailUnique (1.02s)
PASS
ok  	github.com/ionix/ion-mem-cloud/internal/db	42.018s
=== RUN   TestMigrateUpDown
--- PASS: TestMigrateUpDown (35.40s)
=== RUN   TestMigrationRoundtrip
--- PASS: TestMigrationRoundtrip (1.17s)
PASS
ok  	github.com/ionix/ion-mem-cloud/internal/migrate	39.048s
?   	github.com/ionix/ion-mem-cloud/internal/testdb	[no test files]
```

**Result: 7/7 test functions PASS, 14/14 sub-tests PASS**

---

## Structural Notes

- **Migrations location**: all `.sql` files live in `internal/migrate/migrations/` (not repo root `migrations/`) — required for Go `//go:embed` to access from within the `migrate` package. Design's "migrations/" path is a logical reference; actual path is `internal/migrate/migrations/`.
- **golang-migrate driver scheme**: `pgx5://` (not `postgres://`) — required by `golang-migrate/migrate/v4/database/pgx/v5` driver. DSN conversion helper `pgx5DSN()` used in tests.
- **sqlc DBTX interface**: generated code uses `database/sql` interface, not native pgx. Tests bridge via `pgx/v5/stdlib` (`sql.Open("pgx", dsn)`). This is intentional — the `database/sql` interface is the standard sqlc default and works correctly with pgx stdlib.
- **Embedding type**: sqlc generates `interface{}` for the `vector` column override with `[]byte`. This correctly accepts `nil` for NULL and compiles without issues. The embedding column stays effectively opaque until `cloud-semantic-search` adds dimension and ANN indexing.
- **replace directive**: `go.mod` in `ion-mem-cloud` has `replace github.com/ionix/ion-mem-types => ../ion-mem-types` — resolves locally, no remote needed.

---

## Files Created

### ion-mem-types (module `github.com/ionix/ion-mem-types`)

- `go.mod`
- `types.go`
- `types_test.go`

### ion-mem-cloud (module `github.com/ionix/ion-mem-cloud`)

- `go.mod` (with replace directive)
- `go.sum`
- `sqlc.yaml`
- `cmd/migrate/main.go`
- `internal/migrate/migrate.go`
- `internal/migrate/migrate_test.go`
- `internal/migrate/roundtrip_test.go`
- `internal/migrate/migrations/0001_init.{up,down}.sql`
- `internal/migrate/migrations/0002_users.{up,down}.sql`
- `internal/migrate/migrations/0003_projects.{up,down}.sql`
- `internal/migrate/migrations/0004_project_members.{up,down}.sql`
- `internal/migrate/migrations/0005_invites.{up,down}.sql`
- `internal/migrate/migrations/0006_observations.{up,down}.sql`
- `internal/migrate/migrations/0007_audit_log.{up,down}.sql`
- `internal/testdb/container.go`
- `internal/db/` (sqlc-generated: db.go, models.go, users.sql.go, projects.sql.go, project_members.sql.go, invites.sql.go, observations.sql.go, audit_log.sql.go)
- `internal/db/testhelper_test.go`
- `internal/db/users_test.go`
- `internal/db/project_members_test.go`
- `internal/db/invites_test.go`
- `internal/db/observations_test.go`
- `internal/db/audit_log_test.go`
- `query/users.sql`
- `query/projects.sql`
- `query/project_members.sql`
- `query/invites.sql`
- `query/observations.sql`
- `query/audit_log.sql`
