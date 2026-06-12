---
ion-sdd-version: "1.0"
phase: ion-sdd-propose
generated: "2026-06-09T00:00:00Z"
mode: hybrid
change: "cloud-data-model"
topic_key: "ion-sdd/cloud-data-model/proposal"
type: architecture
---

# Cloud data model: the Postgres foundation for shared ion-mem memory

Define the persistence schema for `ion-mem-cloud` — a separate Postgres-backed server that becomes the write arbiter for a single Ionix company instance. This change lands ONLY the data model: tables, RBAC roles, migrations, type-sharing, and integration-test strategy. No HTTP, no sync wire, no auth wire, no embeddings — those are named follow-on changes. The model is the contract every later cloud change builds on.

## Intent

Give multi-developer teams a server schema that can hold users, projects, membership, invites, an audit trail, and server-side observations (mirroring the client plus cloud-only fields). Hybrid operating mode: local SQLite stays the read source of truth, writes go cloud-primary, last-writer-wins per `topic_key`. The cloud arbitrates writes.

## Scope

| In scope | Out of scope (future change) |
|---|---|
| Postgres schema: `users`, `projects`, `project_members`, `invites`, `audit_log`, `observations` | HTTP/REST endpoints — `cloud-rest-api` |
| RBAC roles (`owner` / `editor` / `viewer`) | Sync push/pull wire + outbox protocol — `cloud-sync-protocol` |
| golang-migrate migrations | JWT / sessions / API keys / auth wire — `cloud-auth-wire` |
| Type-sharing strategy decision | Embedding generation + pgvector population — `cloud-semantic-search` |
| Integration-test strategy (testcontainers) | Backups / exports |
| Vector-READY column (nullable, unpopulated) | — |

## Schema sketch

Columns are the key/load-bearing set, not full DDL (design owns DDL).

| Table | Key columns | Relationships |
|---|---|---|
| `users` | `id` (uuid v7), `email` (unique), `external_subject` (nullable), `display_name`, `created_at`, `deleted_at` | referenced by all author/actor FKs |
| `projects` | `id` (uuid v7), `name`, `slug` (unique), `created_by` → users, `created_at`, `deleted_at` | parent of members, invites, observations |
| `project_members` | `id`, `project_id` → projects, `user_id` → users, `role` (enum), `created_at`, `deleted_at` (soft) | unique (`project_id`,`user_id`) |
| `invites` | `id`, `project_id` → projects, `email`, `token` (unique, single-use), `role`, `invited_by` → users, `expires_at`, `accepted_at`, `created_at` | TTL via `expires_at` |
| `audit_log` | `id`, `actor_id` → users (nullable), `action`, `target_type`, `target_id`, `project_id`, `occurred_at`, `metadata` (jsonb) | append-only |
| `observations` | client mirror: `sync_id` (unique), `type`, `title`, `content`, `tool_name`, `project_id` → projects, `scope`, `topic_key`, `normalized_hash`, `revision_count`, `duplicate_count`, `created_at`, `updated_at`, `deleted_at`; cloud-only: `author_id` → users (nullable), `embedding` (nullable vector, unpopulated) | LWW per (`project_id`,`scope`,`topic_key`) |

## Product decisions (default + counter-argument)

| Decision | Default | Counter-argument |
|---|---|---|
| User identity shape | Delegated. `users` holds `email` + nullable `external_subject` (SSO/OAuth subject); NO local password hash. | A local hash avoids an IdP dependency for a tiny first deploy. Rejected: storing credentials invites breach liability; auth wire is a separate change, so leave the column out now rather than migrate it away later. |
| Invites | Single-use link `token` (opaque, unique) carrying `role`, plus the target `email` for display/match. Default TTL **7 days**. | Pure email-list invites need no token table. Rejected: tokens work before the IdP exists and survive email-address mismatches; 7d balances security against onboarding friction. |
| `audit_log` granularity | **Writes only** (create/update/delete/membership/invite). Columns: `actor_id`, `action`, `target_type`, `target_id`, `project_id`, `occurred_at`, `metadata` jsonb. No before/after snapshot. | Logging reads gives full forensic replay. Rejected: read volume from AI agents is huge and low-value; before/after doubles row size — `metadata` jsonb can carry a diff later if needed. |
| `project_members` lifecycle | **Soft-delete** (`deleted_at`). | Hard-delete is simpler and truly removes access. Rejected: soft-delete preserves `audit_log` FK integrity and lets owners see "who was removed when"; re-invite keeps history. |
| `author_id` on legacy observations | **Nullable.** Existing client rows (no author) sync up with `author_id = NULL`, interpreted as "legacy / pre-cloud". | A synthetic "legacy" user row makes every query non-null. Rejected: NULL is honest, needs no seed data, and reads tolerate it (filter is by `type`/`scope`, not author). |

## Approach

- **Backend**: Postgres, pgvector-ready. `observations.embedding` is a nullable vector column left unpopulated this change (zero storage cost until `cloud-semantic-search`).
- **IDs**: uuid v7 (time-ordered) for all server PKs — sortable, conflict-friendly, addresses the explore risk over client `sync_id`. `sync_id` stays the cross-system identity for observations.
- **Migrations**: golang-migrate, forward + down per step, checked into `ion-mem-cloud`.
- **Type-sharing**: **shared module** `github.com/ionix/ion-mem-types` holding wire DTOs only (no storage logic). Counter-argument: duplication avoids cross-repo version coupling; protobuf adds a codegen step. Rejected both — a thin hand-written types module is the lowest-friction boundary between two repos owned by one team, and avoids drift that duplication guarantees.
- **Read-time filtering**: cloud returns everything; clients filter by `type`/`scope`. No write-time quality gating.
- **Tests**: integration tests against a real Postgres via **testcontainers-go** (ephemeral, CI-friendly, no shared dev DB). docker-compose is the fallback for local manual runs.

## Capabilities (contract for ion-sdd-spec)

### New capabilities

- `cloud-persistence` — server schema for users, projects, membership, invites, audit log, and server-side observations on Postgres, with golang-migrate migrations and integration tests.
- `cloud-rbac` — `owner` / `editor` / `viewer` per-project roles enforced at the data layer. `owner` manages members/invites/project; `editor` reads + writes observations; `viewer` reads only. Three roles cover real team needs without the dead weight of a permission matrix.

### Modified capabilities

- None. (Client SQLite schema is untouched this change; `author_id` mapping is a sync-time concern owned by `cloud-sync-protocol`.)

## Success criteria

- [ ] golang-migrate applies all migrations up and down cleanly against a fresh Postgres.
- [ ] All six tables exist with FK integrity, soft-delete columns, and the three RBAC roles.
- [ ] `observations` carries every client field plus `author_id` (nullable) and `embedding` (nullable, unpopulated).
- [ ] Integration tests run green against a testcontainers Postgres in CI.
- [ ] `ion-mem-types` module compiles and is importable from both repos.

## Rollback plan

- Schema lives in a new repo `ion-mem-cloud` with no production data yet — rollback is `migrate down` to baseline, or drop the database.
- The shared `ion-mem-types` module is additive; reverting is deleting the module/import — no client code depends on it yet.
- Client SQLite is unchanged, so there is nothing to revert on developer machines.

## Risks / open questions

- **PR budget**: six tables + migrations + integration test harness + the new types module likely exceeds the 400-line budget. Suggested sub-split: PR-1 `users`/`projects`/`project_members`/`invites` + RBAC + migrations + test harness; PR-2 `observations` + `audit_log` + vector-ready column + `ion-mem-types`. Tasks phase should confirm the cut.
- `ion-mem-cloud` repo and `ion-mem-types` module names are assumed (`github.com/ionix/...`) — confirm before scaffolding.
- pgvector column type (`vector(1536)` vs separate `observation_vectors` table) is deferred to design; embedding model is not yet fixed.
- Engram persistence was BLOCKED in this run (active MCP is `ion-mem`/`ion_*`; the skill references the disabled `engram`/`mem_*`). Proposal persisted to file only — orchestrator must save to engram under `ion-sdd/cloud-data-model/proposal`.
