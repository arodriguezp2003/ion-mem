---
ion-sdd-version: "1.0"
phase: ion-sdd-design
generated: "2026-06-11T00:00:00Z"
mode: hybrid
change: "cloud-data-model"
---

# Design: Cloud data model — Postgres foundation for shared ion-mem memory

## Technical Approach

Land the Postgres schema for the new `ion-mem-cloud` repo (module `github.com/ionix/ion-mem-cloud`) plus a thin shared DTO module `github.com/ionix/ion-mem-types`. This change delivers DDL, golang-migrate migrations, a generated query layer, and a testcontainers integration harness — no service, sync, or auth wire (proposal Scope). The `observations` table mirrors the client `internal/store` struct field-for-field so `cloud-sync-protocol` maps cleanly; client `project` (text) becomes `project_id` (FK) and the client-local `session_id` is dropped (not meaningful server-side).

## Architecture Decisions

### Decision: Postgres access layer — sqlc

| Aspect | Detail |
|---|---|
| **Choice** | `sqlc` (codegen) over `pgx`, with `pgx/v5` as the underlying driver/pool. |
| **Alternatives considered** | Raw `pgx` (hand-written SQL + scans); `database/sql` + squirrel query builder. |
| **Rationale** | The client already writes hand-rolled SQL against `database/sql` (`internal/store`); raw pgx would repeat that boilerplate and scan-drift risk for 6 tables. sqlc gives compile-time-checked, type-safe queries from plain `.sql` files — a small team gets schema/query parity for free and the generated structs become the natural seam the future service layer consumes. pgx is the recommended sqlc driver and keeps a fast path open. This is the load-bearing call; every later cloud change writes queries through sqlc. |

### Decision: pgvector column shape — inline `vector` on `observations`

| Aspect | Detail |
|---|---|
| **Choice** | Inline `embedding vector` column on `observations`, **nullable, dimension deferred** (declared as unconstrained `vector` now; `cloud-semantic-search` adds `ALTER` to `vector(N)` + the ANN index once the model fixes N). |
| **Alternatives considered** | Separate `observation_vectors(observation_id, embedding)` table. |
| **Rationale** | One embedding per observation is a strict 1:1 — a side table adds a join with zero modelling benefit. Nullable + unpopulated costs zero storage until populated (proposal). Deferring dimension avoids guessing the model; sqlc treats `vector` as an opaque type via a custom override, so the column compiles today without pgvector ANN indexing. |

### Decision: uuid v7 generation — in Go (`github.com/google/uuid`)

| Aspect | Detail |
|---|---|
| **Choice** | Generate uuid v7 in application code via `google/uuid` `NewV7()`; PK columns are `uuid NOT NULL` with no DB default. |
| **Alternatives considered** | Postgres-side generation (`pg_uuidv7` extension or 18+ `uuidv7()`). |
| **Rationale** | google/uuid is already the de-facto Go uuid lib and ships v7. App-side generation needs no extension install on the DB host (one fewer ops dependency than pgvector already imposes) and lets the future service know the id before INSERT (returning round-trips avoided). Time-ordered v7 keeps B-tree PK inserts append-friendly. |

### Decision: RBAC enforcement point — schema-shaped now, guarded later

| Aspect | Detail |
|---|---|
| **Choice** | THIS change enforces only structural invariants: a `project_role` enum (`owner`/`editor`/`viewer`), the `project_members` row binding user↔project↔role with a unique `(project_id, user_id)` partial index, and FK integrity. Permission *checks* (who may write an observation, manage invites) are explicitly deferred to `cloud-rest-api`/`cloud-auth-wire`. |
| **Alternatives considered** | Postgres Row-Level Security policies now; a DB-layer guard function. |
| **Rationale** | RLS needs a session-user identity that only the auth wire establishes — wiring it here would be dead, untestable policy. The data model's job is to make illegal *states* unrepresentable (no orphan members, one role per membership); making illegal *actions* unrepresentable is the service layer's job. Keeping the boundary explicit prevents scope creep into auth. |

### Decision: Migration organization — one numbered pair per table, grouped run

| Aspect | Detail |
|---|---|
| **Choice** | golang-migrate, one `{NNNN}_{table}.up.sql` / `.down.sql` pair per table, applied in FK-dependency order. Enums + extensions live in migration `0001_init`. |
| **Alternatives considered** | One mega-migration; grouping by PR slice. |
| **Rationale** | Per-table files keep each migration reviewable and let the PR sub-split (proposal: PR-1 users/projects/members/invites, PR-2 observations/audit_log) map to file ranges without rework. Numbered ordering enforces FK creation order deterministically. |

## Data Flow

    ion-mem-types (DTOs)  ──imported by──►  ion-mem-cloud
                                                │
    .sql queries ──sqlc generate──► internal/db (typed Go)
                                                │
                              golang-migrate ──► Postgres (pgx pool)
                                                ▲
                       testcontainers ephemeral PG (integration tests)

## File Changes (new repo `ion-mem-cloud`)

| File | Action | Description |
|---|---|---|
| `go.mod` | Create | module `github.com/ionix/ion-mem-cloud` |
| `migrations/0001_init.up/down.sql` | Create | `CREATE EXTENSION pgvector`; `project_role` enum |
| `migrations/0002_users.{up,down}.sql` | Create | `users` |
| `migrations/0003_projects.{up,down}.sql` | Create | `projects` |
| `migrations/0004_project_members.{up,down}.sql` | Create | membership |
| `migrations/0005_invites.{up,down}.sql` | Create | invites |
| `migrations/0006_observations.{up,down}.sql` | Create | observations + embedding |
| `migrations/0007_audit_log.{up,down}.sql` | Create | audit log |
| `sqlc.yaml` | Create | pgx engine; `vector`→`[]byte`/custom type override |
| `query/*.sql` | Create | named queries per table |
| `internal/db/` | Create | sqlc-generated package (committed) |
| `internal/migrate/migrate.go` | Create | embed migrations, run up/down |
| `internal/testdb/container.go` | Create | testcontainers Postgres bootstrap |
| `cmd/migrate/main.go` | Create | CLI to apply migrations |

`ion-mem-types` exports only wire DTOs (`Observation`, `User`, `Project`, `Member`, `Invite`, `Role`) — no storage logic, no DB tags.

## DDL (Postgres-valid)

```sql
-- 0001_init
CREATE EXTENSION IF NOT EXISTS vector;
CREATE TYPE project_role AS ENUM ('owner','editor','viewer');

-- 0002 users
CREATE TABLE users (
  id               uuid PRIMARY KEY,
  email            text NOT NULL,
  external_subject text,
  display_name     text NOT NULL,
  created_at       timestamptz NOT NULL DEFAULT now(),
  deleted_at       timestamptz
);
CREATE UNIQUE INDEX users_email_key ON users (lower(email)) WHERE deleted_at IS NULL;

-- 0003 projects
CREATE TABLE projects (
  id          uuid PRIMARY KEY,
  name        text NOT NULL,
  slug        text NOT NULL,
  created_by  uuid NOT NULL REFERENCES users(id),
  created_at  timestamptz NOT NULL DEFAULT now(),
  deleted_at  timestamptz
);
CREATE UNIQUE INDEX projects_slug_key ON projects (slug) WHERE deleted_at IS NULL;

-- 0004 project_members
CREATE TABLE project_members (
  id          uuid PRIMARY KEY,
  project_id  uuid NOT NULL REFERENCES projects(id),
  user_id     uuid NOT NULL REFERENCES users(id),
  role        project_role NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT now(),
  deleted_at  timestamptz
);
CREATE UNIQUE INDEX members_project_user_key
  ON project_members (project_id, user_id) WHERE deleted_at IS NULL;

-- 0005 invites
CREATE TABLE invites (
  id          uuid PRIMARY KEY,
  project_id  uuid NOT NULL REFERENCES projects(id),
  email       text NOT NULL,
  token       text NOT NULL UNIQUE,
  role        project_role NOT NULL,
  invited_by  uuid NOT NULL REFERENCES users(id),
  expires_at  timestamptz NOT NULL DEFAULT now() + interval '7 days',
  accepted_at timestamptz,
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX invites_project_idx ON invites (project_id);

-- 0006 observations  (mirrors client internal/store; project->project_id, no session_id)
CREATE TABLE observations (
  id              uuid PRIMARY KEY,
  sync_id         text NOT NULL UNIQUE,
  type            text NOT NULL,
  title           text NOT NULL,
  content         text NOT NULL,
  tool_name       text,
  project_id      uuid NOT NULL REFERENCES projects(id),
  scope           text NOT NULL DEFAULT 'project',
  topic_key       text,
  normalized_hash text NOT NULL,
  revision_count  integer NOT NULL DEFAULT 1,
  duplicate_count integer NOT NULL DEFAULT 0,
  author_id       uuid REFERENCES users(id),       -- nullable: legacy/pre-cloud
  embedding       vector,                           -- nullable, unpopulated
  last_seen_at    timestamptz NOT NULL,
  created_at      timestamptz NOT NULL,
  updated_at      timestamptz NOT NULL,
  deleted_at      timestamptz
);
CREATE UNIQUE INDEX obs_topic_lww_key
  ON observations (project_id, scope, topic_key) WHERE deleted_at IS NULL AND topic_key IS NOT NULL;
CREATE INDEX obs_dedupe_idx
  ON observations (normalized_hash, project_id, scope, type, title) WHERE deleted_at IS NULL;
CREATE INDEX obs_project_idx ON observations (project_id, created_at DESC);

-- 0007 audit_log  (append-only)
CREATE TABLE audit_log (
  id          uuid PRIMARY KEY,
  actor_id    uuid REFERENCES users(id),
  action      text NOT NULL,
  target_type text NOT NULL,
  target_id   uuid,
  project_id  uuid REFERENCES projects(id),
  occurred_at timestamptz NOT NULL DEFAULT now(),
  metadata    jsonb NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX audit_project_idx ON audit_log (project_id, occurred_at DESC);
```

Note: client FTS5 (`observations_fts` + triggers) is SQLite-only and intentionally NOT mirrored; full-text/semantic search is `cloud-semantic-search`. Client `normalized_hash` semantics (SHA-256 of normalized content) are preserved as a plain `text` column — sync supplies the value.

## Testing Strategy

| Layer | What to test | Approach |
|---|---|---|
| Migration | up then down leaves baseline; up is idempotent-safe in order | testcontainers-go Postgres, run `migrate up`/`down`, assert table/enum existence via `information_schema` |
| Schema (integration) | FK rejects orphan member/observation; unique partial indexes enforce one active membership + one active topic_key row; enum rejects bad role; soft-delete frees the unique slot | table-driven Go integration tests through sqlc queries against testcontainers PG; skip under `testing.Short()` (go-testing skill) |
| DTO (unit) | `ion-mem-types` JSON round-trips; `Role` marshals to the three enum strings | table-driven unit tests, no DB |
| CI | testcontainers spins ephemeral PG; docker-compose fallback for local manual runs | per proposal |

Per go-testing: integration tests table-driven with `t.Run`, gated by `testing.Short()`, asserting state transitions (insert→constraint violation) not implementation trivia.

## Migration / Rollout

No production data exists. Rollback = `migrate down` to baseline or drop the DB. `ion-mem-types` is additive; reverting is deleting the module. Client SQLite untouched.

## Build Sequence

1. Scaffold `ion-mem-types` (DTOs + tests) — importable, compiles.
2. Scaffold `ion-mem-cloud` go.mod + `migrations/0001_init` (extension, enum) + `internal/migrate` + `internal/testdb`.
3. Migrations `0002`–`0005` (users/projects/members/invites) + sqlc queries + schema integration tests → PR-1.
4. Migrations `0006`–`0007` (observations + author_id + embedding, audit_log) + sqlc + tests → PR-2.

## Open Questions

- [ ] Confirm module paths `github.com/ionix/ion-mem-cloud` and `github.com/ionix/ion-mem-types` before scaffolding (assumed).
- [ ] Embedding dimension N deferred to `cloud-semantic-search`; column stays unconstrained `vector` until then — confirm no ANN index is wanted in this change (assumed none).
