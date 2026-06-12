---
ion-sdd-version: "1.0"
phase: ion-sdd-spec
generated: "2026-06-11T00:00:00Z"
mode: hybrid
change: "cloud-data-model"
capability: "cloud-rbac"
spec_kind: full
---

# cloud-rbac Specification

## Purpose

Defines per-project role-based access control enforced at the data layer. Three roles — `owner`, `editor`, `viewer` — govern what operations are permitted on a project. Role assignment lives in `project_members`; enforcement is a data-layer contract, not an HTTP middleware concern.

## Requirements

### Requirement: Three-Role Enum

The `project_members.role` column MUST accept exactly three values: `owner`, `editor`, and `viewer`. Any attempt to store a value outside this set MUST be rejected by the database.

#### Scenario: Valid role stored

- GIVEN a user is added to a project
- WHEN `role` is set to `owner`, `editor`, or `viewer`
- THEN the row is stored successfully

#### Scenario: Invalid role rejected

- GIVEN a membership record is being inserted
- WHEN `role` is set to an unlisted value (e.g., `admin`)
- THEN the database rejects the insert with a type or check constraint error

---

### Requirement: Owner Permissions

A project member with role `owner` MUST be permitted to manage project membership (add/remove members), manage invites (create/revoke), and update project-level metadata. An `owner` MUST also have all permissions of `editor` and `viewer`.

#### Scenario: Owner adds a member

- GIVEN a user has role `owner` on a project
- WHEN a new member is added to that project
- THEN the membership record is created successfully

#### Scenario: Owner creates an invite

- GIVEN a user has role `owner` on a project
- WHEN an invite is created for that project
- THEN the invite record is stored with the owner's `user_id` as `invited_by`

---

### Requirement: Editor Permissions

A project member with role `editor` MUST be permitted to read and write observations within the project. An `editor` MUST also have all permissions of `viewer`. An `editor` MUST NOT be permitted to manage project membership or invites.

#### Scenario: Editor writes an observation

- GIVEN a user has role `editor` on a project
- WHEN an observation is upserted for that project
- THEN the observation is stored successfully

#### Scenario: Editor cannot manage membership

- GIVEN a user has role `editor` on a project
- WHEN the user attempts to add or remove a project member
- THEN the operation is not permitted at the data-layer contract level

---

### Requirement: Viewer Permissions

A project member with role `viewer` MUST be permitted to read observations within the project. A `viewer` MUST NOT be permitted to write, update, or delete observations, manage membership, or manage invites.

#### Scenario: Viewer reads observations

- GIVEN a user has role `viewer` on a project
- WHEN observations for that project are queried
- THEN the observations are returned successfully

#### Scenario: Viewer cannot write observations

- GIVEN a user has role `viewer` on a project
- WHEN an observation upsert is attempted for that project
- THEN the operation is not permitted at the data-layer contract level

---

### Requirement: Role Uniqueness Per Project Member

Each (`project_id`, `user_id`) pair MUST have at most one active membership row (where `deleted_at IS NULL`). A user MUST NOT hold two simultaneous roles on the same project.

#### Scenario: Duplicate active membership rejected

- GIVEN a user is an active member of a project with role `editor`
- WHEN a second active membership row for the same (`project_id`, `user_id`) is inserted
- THEN the database rejects the insert due to a unique constraint

#### Scenario: Re-invite after soft-delete is valid

- GIVEN a user's membership has been soft-deleted (`deleted_at` is set)
- WHEN a new active membership row is inserted for the same (`project_id`, `user_id`)
- THEN the insert succeeds because the previous row is not active

---

### Requirement: Invite Role Propagates to Membership

When an invite is accepted, the membership row created MUST use the `role` stored on the invite record. The accepting user's role MUST match the invite's `role` exactly.

#### Scenario: Invite accepted creates membership with correct role

- GIVEN an invite exists with `role = viewer`
- WHEN the invite is accepted by a user
- THEN a `project_members` row is created with `role = viewer` for that user
- AND the invite's `accepted_at` is set

---

### Requirement: Single-Tenant Scope

All RBAC enforcement MUST be scoped to the single tenant represented by this deployment. Cross-tenant role checks are out of scope. The data model MUST NOT include a tenant discriminator column.

#### Scenario: No tenant column in schema

- GIVEN the schema is applied to a single-tenant deployment
- WHEN the `project_members`, `projects`, or `invites` tables are inspected
- THEN no `tenant_id` or equivalent multi-tenant discriminator column is present
