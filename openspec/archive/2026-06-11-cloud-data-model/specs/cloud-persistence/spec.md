---
ion-sdd-version: "1.0"
phase: ion-sdd-spec
generated: "2026-06-11T00:00:00Z"
mode: hybrid
change: "cloud-data-model"
capability: "cloud-persistence"
spec_kind: full
---

# cloud-persistence Specification

## Purpose

Defines the server-side Postgres data model for ion-mem-cloud: the six core tables (`users`, `projects`, `project_members`, `invites`, `audit_log`, `observations`), schema migration behavior, integration-test requirements, and the shared types module boundary. This capability is the foundational contract for all subsequent cloud changes.

## Requirements

### Requirement: User Identity Shape

The `users` table MUST store a unique `email` and a nullable `external_subject` (SSO/OAuth subject) per user. The `users` table MUST NOT store a password hash.

#### Scenario: New user with SSO subject

- GIVEN a user record is being created
- WHEN an `external_subject` value is provided
- THEN the record is saved with `email`, `external_subject`, and a uuid v7 `id`
- AND no password-related column is present in the schema

#### Scenario: Legacy user without SSO subject

- GIVEN a user record is being created
- WHEN no `external_subject` value is provided
- THEN the record is saved with `email` and `external_subject` as NULL
- AND the record is valid and retrievable

---

### Requirement: UUID v7 Server Primary Keys

All server-side table primary keys MUST be uuid v7. Client-originated cross-system identity for observations MUST use a separate `sync_id` column, not the server PK.

#### Scenario: New record insertion

- GIVEN a new row is inserted into any server table
- WHEN the insert completes
- THEN the row's `id` is a time-ordered uuid v7

#### Scenario: Observation sync identity

- GIVEN an observation is synced from a client
- WHEN the row is stored
- THEN `sync_id` holds the client-originated identifier and `id` is the server-assigned uuid v7

---

### Requirement: Foreign Key Integrity Across Tables

All cross-table references MUST be enforced as FK constraints. `project_members.project_id` → `projects`, `project_members.user_id` → `users`, `invites.project_id` → `projects`, `invites.invited_by` → `users`, `observations.project_id` → `projects`, `observations.author_id` → `users` (nullable).

#### Scenario: FK violation rejected

- GIVEN a `project_members` record is inserted
- WHEN the referenced `project_id` does not exist
- THEN the database rejects the insert with a FK constraint error

#### Scenario: Nullable FK allows NULL

- GIVEN an observation from a pre-cloud client is synced
- WHEN `author_id` is NULL
- THEN the row is stored successfully without violating FK constraints

---

### Requirement: Soft-Delete for project_members

`project_members` MUST support soft-delete via a `deleted_at` timestamp column. Active membership MUST be queried by filtering `deleted_at IS NULL`. Soft-deleted rows MUST remain in the table to preserve audit_log FK integrity.

#### Scenario: Member removal

- GIVEN a user is an active member of a project
- WHEN the member is removed
- THEN `deleted_at` is set to the current timestamp
- AND the row remains in the table

#### Scenario: Active membership query

- GIVEN a project has both active and removed members
- WHEN active members are queried
- THEN only rows with `deleted_at IS NULL` are returned

---

### Requirement: Single-Use Invite Token with TTL

The `invites` table MUST store a unique, opaque token per invite. Each token MUST be single-use: once accepted (`accepted_at` set), no subsequent use is valid. Invites MUST have an `expires_at` timestamp defaulting to 7 days from creation.

#### Scenario: Token uniqueness enforced

- GIVEN an invite token value is generated
- WHEN a second invite with the same token is inserted
- THEN the database rejects the insert due to a unique constraint on `token`

#### Scenario: Invite acceptance marks token used

- GIVEN a valid, unexpired invite exists with `accepted_at` NULL
- WHEN the invite is accepted
- THEN `accepted_at` is set to the current timestamp
- AND any further acceptance attempt with the same token MUST be rejected

#### Scenario: Expired invite

- GIVEN an invite where `expires_at` is in the past
- WHEN the invite is evaluated
- THEN it is treated as invalid regardless of `accepted_at`

---

### Requirement: Audit Log Append-Only

The `audit_log` table MUST be write-only at the data-layer contract level. Rows MUST NOT be updated or deleted by the application. Required columns: `actor_id` (→ users, nullable), `action`, `target_type`, `target_id`, `project_id`, `occurred_at`, `metadata` (jsonb). No before/after snapshot columns.

#### Scenario: Audit entry written

- GIVEN a write action occurs (create/update/delete/membership/invite)
- WHEN the audit entry is persisted
- THEN a new row is appended with the required columns populated
- AND no existing audit row is modified

#### Scenario: System action with no actor

- GIVEN a system-initiated action has no user actor
- WHEN the audit entry is written
- THEN `actor_id` is NULL and the row is valid

---

### Requirement: Observations Table Schema

The `observations` table MUST include all client-side fields (`sync_id` unique, `type`, `title`, `content`, `tool_name`, `project_id`, `scope`, `topic_key`, `normalized_hash`, `revision_count`, `duplicate_count`, `created_at`, `updated_at`, `deleted_at`) plus cloud-only fields: `author_id` (→ users, nullable) and `embedding` (nullable vector column, unpopulated this change).

#### Scenario: Client observation sync

- GIVEN a client observation is synced to the cloud
- WHEN the row is stored
- THEN all client-side fields are persisted and `sync_id` is unique per project
- AND `author_id` may be NULL for pre-cloud observations

#### Scenario: Vector column present but unpopulated

- GIVEN the schema is applied
- WHEN an observation row is inserted
- THEN the `embedding` column exists and defaults to NULL
- AND no error occurs from the NULL embedding

---

### Requirement: Migration Reversibility

Every golang-migrate migration file MUST include a working down migration. Applying all up migrations followed by all down migrations against a fresh Postgres instance MUST return the schema to baseline without error.

#### Scenario: Up-then-down roundtrip

- GIVEN a fresh Postgres instance with no application schema
- WHEN all migration up steps are applied, then all down steps are applied
- THEN no migration step returns an error
- AND the schema is at baseline after the down pass

---

### Requirement: Integration Tests via Testcontainers

Integration tests for schema migrations and data-layer invariants MUST run against a real Postgres instance provided by testcontainers-go. Tests MUST NOT require a shared developer database or an external Postgres service running on the host.

#### Scenario: CI integration test run

- GIVEN a clean CI environment with Docker available
- WHEN the integration test suite is executed
- THEN testcontainers-go provisions an ephemeral Postgres container
- AND all migration and FK/uniqueness/soft-delete invariant tests pass against it
- AND the container is torn down after the suite completes

---

### Requirement: Shared Types Module Boundary

A shared Go module (`ion-mem-types`) MUST export wire DTO types only. It MUST NOT contain storage logic, migration code, or database drivers. It MUST be importable from both `ion-mem` and `ion-mem-cloud` without circular dependencies.

#### Scenario: Module import from both repos

- GIVEN `ion-mem-types` is published
- WHEN `ion-mem` and `ion-mem-cloud` each import the module
- THEN both compile successfully
- AND neither experiences a circular dependency

#### Scenario: No storage logic in types module

- GIVEN the types module is reviewed
- WHEN its exported symbols are enumerated
- THEN no database driver, query, or migration reference is present
