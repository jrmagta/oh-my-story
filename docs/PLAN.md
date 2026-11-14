# oh-my-story — development plan

A self-hosted user story mapping tool. Feature set modelled on Avion; visual design inspired by leidos.com (Australia).

## Decisions

| Area | Decision |
|---|---|
| Backend | Go, single static binary, web app embedded |
| Database | PostgreSQL 16 |
| Realtime | v1. WebSocket fan-out driven by Postgres `LISTEN/NOTIFY`; presence; last-write-wins per field |
| Auth | OIDC (Entra ID / Keycloak / Okta) + local accounts |
| Frontend | React + TypeScript + Vite, TanStack Query, Zustand, React Router, dnd-kit, Tailwind with design tokens |
| Dev deploy | docker-compose: app, Postgres, Keycloak, Mailpit |
| Prod deploy | Helm chart: Deployment, Service, Ingress, HPA, migration Job; external Postgres or CloudNativePG |
| Data model | `docs/DATA_MODEL.md`. UUIDv7 ids, fractional-index ranks, per-workspace roles, invite-only sign-up, workspace-wide story keys |
| Target devices | Desktop browser first. Phone and tablet layouts are not optimised; the map scrolls sideways and may be view-only there |
| Tests | Go tests against real Postgres, Playwright e2e |

## Design tokens (from leidos.com CSS)

| Token | Value | Use |
|---|---|---|
| indigo-900 | `#201747` | App shell, backbone row |
| indigo-800 | `#251846` | Dark surfaces |
| magenta-600 | `#850F89` | Primary action, selection |
| magenta-500 | `#901588` | Hover |
| violet-600 | `#4C3291` | Secondary |
| lavender-300 | `#B9A5EE` | Releases, subtle accents |
| teal-600 | `#00778B` | Personas, info |
| crimson-600 | `#A61747` | Blocked, destructive |
| grey-50 / 900 | `#F5F5F5` / `#262629` | Canvas / text |

Type: Avenir Next is licensed; use self-hosted Nunito Sans unless a licence is available. Square corners, card modules, dark hero bands over light content.

## Features

Status: **in** (v1), **later**, **out**, **?** (undecided).

### Tier 0 — the map
| Feature | Status |
|---|---|
| Backbone of 3 or 4 levels, chosen per map (see below) | in |
| Release slices | in |
| Drag and drop across cells and releases | in |
| Story details: title, markdown description, status | in |
| Workspaces → Maps, members, roles | in |

### Backbone hierarchy

| Level | 4-level map | 3-level map | Jira Data Center (future sync) |
|---|---|---|---|
| Objective: groups activities | yes | — | Initiative (Advanced Roadmaps) |
| Activity | yes | yes | Epic |
| Step | yes | yes | Label on stories, e.g. `step:log-in` |
| Story | yes | yes | Story |

Model the backbone as one ordered tree of nodes with a `level`. Each node and story carries a nullable external reference (system, key, URL), so sync can be added later without a migration that changes the data's shape.

### Tier 1 — planning essentials
| Feature | Status |
|---|---|
| Estimates with roll-ups | in |
| Personas linked to activities | in |
| Comments | in |
| Labels / tags | in |
| Filter and highlight | in |
| Search | in |
| Zoom out / collapse / focus a release | in |
| Clone map | out |
| Read-only public share link | in |
| PDF/PNG export | in |
| CSV import/export | out |

### Tier 2 — Avion differentiators
| Feature | Status |
|---|---|
| Real-time editing and presence | in |
| Dependencies / blockers drawn on map | in |
| Linked maps (story in several maps) | in |
| Roadmap view synced with map | in |
| Custom status workflows | in |
| Comment-only viewer role | in |
| Saved, shareable filtered views | in |
| Attachments, Figma embeds | later |
| Activity / audit history | in |

### Tier 3 — integrations
| Feature | Status |
|---|---|
| Public REST API + outbound webhooks | in |
| Jira Data Center sync (mapping above) | later |
| Azure DevOps, GitHub, Linear, Trello | later |
| Mattermost notifications | in |
| Slack / Teams notifications | later |

## Architecture choices driven by scope

- **One change-event log.** Every mutation appends a row to `events` (actor, entity, before/after) in the same transaction, then `NOTIFY`. That single table feeds real-time sync, audit history, webhooks and Mattermost.
- **Stories are separate from where they sit on a map.** A `placements` table (story, map, step, release, rank) lets one story appear in several maps (linked maps) without copying it.
- **Statuses are data, not an enum,** so custom workflows and Jira status mapping need no schema change.

## Milestones (proposed)

| # | Milestone | Contents |
|---|---|---|
| M0 | Foundations | Repo layout, Go server, migrations, docker-compose (Postgres, Keycloak, Mailpit), local + OIDC auth, workspaces and roles (incl. comment-only), CI, Helm skeleton |
| M1 | The map | Backbone of 3 or 4 levels, releases, stories, drag and drop, story details, custom statuses, event log, real-time sync and presence, audit history |
| M2 | Planning | Estimates and roll-ups, personas, labels, comments and @mentions, search, filter and highlight, saved views, zoom/collapse/focus |
| M3 | Sharing | Public share links, PDF/PNG export, roadmap view |
| M4 | Connected | Dependencies, linked maps, REST API with tokens and OpenAPI, webhooks, Mattermost |
| Later | | Jira Data Center sync, attachments and Figma, other integrations |
