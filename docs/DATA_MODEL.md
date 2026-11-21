# oh-my-story — data model

Full v1 model, with the milestone in which each table is first migrated. Source of truth for scope is `PLAN.md`; this file only shapes the data.

## Conventions

- **IDs:** `uuid`, generated app-side as UUIDv7 (time-ordered, index-friendly). Exposed as-is in the API.
- **Timestamps:** `timestamptz`, `created_at` / `updated_at` on every mutable table. UTC.
- **Ordering:** `rank text` using fractional indexing (lexicographic, e.g. `a0`, `a0V`, `a1`). A move rewrites one row, not its siblings. Collate `"C"`, unique among siblings (`NULLS NOT DISTINCT` where the scope has a nullable column).
- **Soft delete:** `deleted_at timestamptz` on `maps`, `nodes`, `steps`, `comments`. Everything else hard-deletes. The `events` log keeps the history either way.
- **Tenancy:** every row reaches a `workspace_id`, directly or through its map. Queries always filter by it.
- **External refs:** `ext_system text, ext_key text, ext_url text` (all nullable; system and key set together) on `nodes` and `steps`. Unique on `(ext_system, ext_key)` where set: per workspace for steps, per map for nodes, which have no `workspace_id`.
- **Concurrency:** last-write-wins per field. `version int` on `steps` and `nodes`, bumped on write, used only to detect and report stale drags, not to reject edits.

## Entity overview

```mermaid
erDiagram
  users ||--o{ identities : has
  users ||--o{ workspace_members : joins
  workspaces ||--o{ workspace_members : has
  workspaces ||--o{ maps : owns
  workspaces ||--o{ workflows : owns
  workflows ||--o{ statuses : has
  maps }o--|| workflows : uses
  maps ||--o{ nodes : "backbone"
  nodes ||--o{ nodes : parent
  maps ||--o{ releases : has
  workspaces ||--o{ steps : owns
  steps ||--o{ placements : "appears via"
  maps ||--o{ placements : holds
  nodes ||--o{ placements : "activity"
  releases ||--o{ placements : slice
  steps }o--|| statuses : in
  workspaces ||--o{ personas : has
  personas }o--o{ nodes : "persona_nodes"
  workspaces ||--o{ labels : has
  steps }o--o{ labels : "step_labels"
  steps ||--o{ comments : has
  steps ||--o{ dependencies : "from / to"
  maps ||--o{ saved_views : has
  maps ||--o{ share_links : has
  workspaces ||--o{ events : logs
  workspaces ||--o{ api_tokens : has
  workspaces ||--o{ webhooks : has
```

## Key design decisions

1. **Steps belong to the workspace, not the map.** A step's position lives in `placements`. Linked maps are then just a second placement row. A step with zero placements is a backlog item.
2. **Backbone is one `nodes` table.** `level` is `objective | activity`. A constraint trigger enforces the parent: an activity sits under an objective of the same map, an objective has none. Steps are not nodes: they are cards placed under an activity, in a release.
3. **Statuses are rows.** `workflows` own ordered `statuses`, each with a `category` (`todo | doing | done | blocked`) so roll-ups and the roadmap never depend on custom names. A map points at one workflow.
4. **Roles are per workspace** in v1. Map-level overrides are deferred: add a nullable `map_members` table later without touching existing rows.
5. **`events` is append-only and the single feed** for real-time, audit, webhooks and Mattermost. Written in the same transaction as the mutation, then `pg_notify('events', id)`.

## Tables

### Identity and access (M0)

```sql
users (
  id uuid pk, email citext unique not null, display_name text not null,
  avatar_url text, password_hash text,          -- null for OIDC-only users
  is_active bool not null default true,
  created_at, updated_at
)

identities (                                     -- OIDC links
  id uuid pk, user_id fk users on delete cascade,
  issuer text not null, subject text not null,
  unique (issuer, subject)
)

sessions (
  id uuid pk, user_id fk, token_hash bytea unique not null,
  expires_at timestamptz not null, user_agent text, ip inet, created_at
)

workspaces (
  id uuid pk, name text not null, slug citext unique not null,
  key_prefix text not null default 'STEP',      -- the project key, as in CHC-142; unique on the server (0007);
                                                 -- suggested from the name: "Cheshire Cat" gives CHC
  next_step_seq int not null default 1,
  estimate_unit text not null default 'points',  -- label only: points, days, ...
  created_at, updated_at
)

workspace_members (
  workspace_id fk, user_id fk,
  role text not null check (role in ('owner','admin','editor','commenter','viewer')),
  primary key (workspace_id, user_id)
)

invites (
  id uuid pk, workspace_id fk, email citext not null, role text not null,
  token_hash bytea unique not null, invited_by fk users,
  expires_at, accepted_at
)

password_resets (                                -- M1: one-hour, single-use links for local accounts
  id uuid pk, user_id fk users on delete cascade,
  token_hash bytea unique not null, expires_at timestamptz not null, used_at, created_at
)
```

Role capabilities: `owner` all incl. delete workspace; `admin` members, workflows, tokens, webhooks; `editor` all map and step edits; `commenter` comments only; `viewer` read only.

### Workflows (M1)

```sql
workflows (
  id uuid pk, workspace_id fk, name text not null, is_default bool not null default false
)                                                -- one default per workspace (partial unique index)

statuses (
  id uuid pk, workflow_id fk on delete cascade,
  name text not null, category text not null check (category in ('todo','doing','done','blocked')),
  colour text, rank text not null,
  ext_name text,                                 -- Jira status name for later sync
  unique (workflow_id, name)
)
```

Deleting a status requires reassigning its steps to another status in the same workflow (enforced in the service, `ON DELETE RESTRICT` in the schema).

### The map (M1)

```sql
maps (
  id uuid pk, workspace_id fk, workflow_id fk workflows,
  name text not null, description text,
  deleted_at, created_at, updated_at
)

nodes (                                          -- backbone
  id uuid pk, map_id fk, parent_id fk nodes,
  level text not null check (level in ('objective','activity')),
  title text not null, description text, colour text,
  rank text not null,                            -- among siblings
  version int not null default 0,
  ext_system, ext_key, ext_url,
  deleted_at, created_at, updated_at
)
-- index (map_id, parent_id, rank)

releases (
  id uuid pk, map_id fk, name text not null, description text,
  target_date date, rank text not null,          -- top to bottom order on the map
  created_at, updated_at
)                                                -- an implicit "Unscheduled" slice is the placement with release_id null

steps (
  id uuid pk, workspace_id fk,
  key text not null,                             -- human key, e.g. STEP-142, unique per workspace
  title text not null, description_md text not null default '',
  status_id fk statuses, estimate numeric(6,2),  -- unit set per workspace (points / days)
  assignee_id fk users, created_by fk users,
  version int not null default 0,
  ext_system, ext_key, ext_url,
  search tsvector generated always as (...) stored,   -- title + description, GIN
  deleted_at, created_at, updated_at,
  unique (workspace_id, key)
)

placements (                                     -- where a step sits on a map
  id uuid pk, step_id fk steps on delete cascade, map_id fk maps,
  activity_id fk nodes,                          -- must be level = 'activity'
  release_id fk releases,                        -- null = unscheduled
  rank text not null,                            -- within the (activity, release) cell
  unique (step_id, map_id)                      -- a step appears once per map
)
-- index (map_id, activity_id, release_id, rank)
```

`workspaces.next_step_seq` backs `steps.key` (incremented in the insert transaction).

### Planning (M2)

```sql
personas (
  id uuid pk, workspace_id fk, name citext not null, description text not null default '', colour text not null,
  unique (workspace_id, name)
)
persona_nodes (                                  -- personas linked to objectives
  persona_id fk, node_id fk,                     -- node.level = 'objective'
  primary key (persona_id, node_id)
)

labels ( id uuid pk, workspace_id fk, name citext not null, colour text not null, unique (workspace_id, name) )
step_labels ( step_id fk, label_id fk, primary key (step_id, label_id) )

comments (
  id uuid pk, step_id fk on delete cascade, author_id fk users on delete set null,
  body_md text not null, parent_id fk comments,  -- one level of threading
  resolved_at, resolved_by fk users, edited_at, deleted_at, created_at
)
mentions ( comment_id fk, user_id fk, primary key (comment_id, user_id) )

saved_views (
  id uuid pk, map_id fk on delete cascade, name text not null, created_by fk users on delete cascade,
  filter jsonb not null,                         -- {labels, statuses, assignees, personas, releases, text, mode}
  is_shared bool not null default false, created_at, updated_at
)
```

Estimate roll-ups are computed, not stored. The browser sums them from the map payload (`web/src/map/estimates.ts`), which events keep current. A server-side roll-up (a recursive CTE over `nodes` through `placements`) can come with the public API if a consumer needs it.

### Sharing (M3)

```sql
share_links (
  id uuid pk, map_id fk, token_hash bytea unique not null,
  created_by fk users, expires_at, revoked_at, created_at
)                                                -- read-only, anonymous, no comments
```

### Connected (M4)

```sql
dependencies (
  id uuid pk, workspace_id fk,
  from_step_id fk steps, to_step_id fk steps,   -- "from blocks to"
  created_by fk users, created_at,
  check (from_step_id <> to_step_id),
  unique (from_step_id, to_step_id)
)

api_tokens (
  id uuid pk, workspace_id fk, name text not null,
  token_hash bytea unique not null, prefix text not null,   -- shown in UI
  scope text not null check (scope in ('read','write')),
  created_by fk users, last_used_at, expires_at, revoked_at, created_at
)

webhooks (
  id uuid pk, workspace_id fk, url text not null, secret text not null,
  event_types text[] not null, is_active bool not null default true, created_at
)
webhook_deliveries (
  id uuid pk, webhook_id fk, event_id fk events,
  status_code int, attempt int not null default 0,
  next_attempt_at, delivered_at, last_error text
)

integrations (                                   -- Mattermost now, others later
  id uuid pk, workspace_id fk, kind text not null,   -- 'mattermost'
  config jsonb not null,                         -- webhook URL etc., secrets encrypted
  event_types text[] not null, is_active bool not null default true
)
```

Dependency cycles are rejected in the service layer (graph walk on insert).

### Event log (M1)

```sql
events (
  id bigint generated always as identity pk,     -- monotonic: clients resume with "after id"
  workspace_id fk, map_id fk null,
  actor_id fk users null,                        -- null for system / API token (see actor_kind)
  actor_kind text not null,                      -- 'user' | 'token' | 'system'
  entity_type text not null,                     -- 'step','node','placement','release','comment',...
  entity_id uuid not null,
  action text not null,                          -- 'create','update','move','delete','restore'
  before jsonb, after jsonb,                     -- changed fields only
  created_at timestamptz not null default now()
)
-- index (workspace_id, id), (entity_type, entity_id, id), (map_id, id)
```

`NOTIFY` carries only the event id. Each app instance reads new rows and fans out to the WebSockets subscribed to that map. Presence is in-memory per instance, shared via the same channel, and never persisted.

Retention: keep forever in v1. Partition by month if volume demands it.

## Migration order

| Migration | Tables | Milestone |
|---|---|---|
| 0001 | extensions (`citext`, `pg_trgm`), `users`, `identities`, `sessions`, `workspaces`, `workspace_members`, `invites` | M0 |
| 0002 | `workflows`, `statuses`, `maps`, `nodes`, `releases`, `steps`, `placements`, `events` | M1 |
| 0003 | `password_resets` | M1 |
| 0004 | `labels`, `step_labels` | M2 phase 2 |
| 0005 | `personas`, `persona_nodes` | M2 phase 3 |
| 0006 | `saved_views` | M2 phase 5 |
| 0007 | unique index on `workspaces.key_prefix` (project keys) | M2 |
| 0008 | `comments`, `mentions` | M2 phase 7 |
| later | `share_links` | M3 |
| later | `dependencies`, `api_tokens`, `webhooks`, `webhook_deliveries`, `integrations` | M4 |

M0 migrates only 0001. The rest are specified here so M1 does not reshape M0 tables.

## Decisions (resolved in review)

| # | Question | Decision |
|---|---|---|
| 1 | Rank scheme | Fractional text keys |
| 2 | Estimate unit | One numeric column, per-workspace unit label (`workspaces.estimate_unit`) |
| 3 | Assignee | Kept: `steps.assignee_id` |
| 4 | Role scope | Per workspace only; no map-level roles in v1 |
| 5 | Sign-up | Invite-only. The first user bootstraps the instance as owner |
| 6 | Soft delete | `maps`, `nodes`, `steps`, `comments` only |
| 7 | Step keys | One workspace-wide counter, and a project key per workspace that is unique on the server (`workspaces.key_prefix`, `next_step_seq`) |
