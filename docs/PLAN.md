# oh-my-story — development plan

A self-hosted user story mapping tool. Feature set modelled on Avion; visual design inspired by blink182.com.

## Decisions

| Area | Decision |
|---|---|
| Backend | Go, single static binary, web app embedded |
| Database | PostgreSQL 16 |
| Realtime | v1. WebSocket fan-out driven by Postgres `LISTEN/NOTIFY`; presence; last-write-wins per field |
| Auth | OIDC (Entra ID / Keycloak / Okta) + local accounts |
| Frontend | React + TypeScript + Vite, TanStack Query, Zustand, React Router, dnd-kit, Tailwind CSS v4 + daisyUI v5 with two custom themes |
| Dev deploy | docker-compose: app, Postgres, Keycloak, Mailpit |
| Prod deploy | Helm chart: Deployment, Service, Ingress, HPA, migration Job; external Postgres or CloudNativePG. A `vX.Y.Z` tag publishes the image `ghcr.io/jrmagta/oh-my-story:X.Y.Z` and the chart `oci://ghcr.io/jrmagta/charts/oh-my-story` (`.github/workflows/release.yml`) |
| Data model | `docs/DATA_MODEL.md`. UUIDv7 ids, fractional-index ranks, per-workspace roles, invite-only sign-up, workspace-wide step keys with a server-unique project key (CHC-12) |
| Target devices | Desktop browser first. Phone and tablet layouts are not optimised; the map scrolls sideways and may be view-only there |
| Tests | Go tests against real Postgres, Playwright e2e |

## Design (from blink182.com)

Mock-up of record: `docs/mockup/map-v3.html`. Earlier attempts kept for reference: `map-leidos.html`, `map-v2.html`.

Brand colours, the same in both themes:

| Token | Value | Use |
|---|---|---|
| red | `#BA3636` | Blocked, destructive, blocked-step panel header |
| amber | `#FCB131` | Primary action, selection, in progress, alternate release band |
| green | `#549334` | Done, secondary action, active filter, first release band |
| ink | `#000000` | Nav bar, rings around circles, Unscheduled band |
| grey | `#8A8A93` | To do |

Two daisyUI themes, `blink-dark` and `blink-light`. The theme follows the system preference, and a toggle in the nav overrides it. Only surfaces change:

| Surface | blink-dark | blink-light |
|---|---|---|
| Canvas | `#1B1B1D` | `#FFFFFF` |
| Raised / header | `#141416` | `#F2F2F2` |
| Card | `#26262A`, 1px `#3A3A40` border | `#FFFFFF`, 2px `#1B1B1D` border |
| Text / muted | `#F2F2F2` / `#A1A1AA` | `#1B1B1D` / `#5F5F68` |
| Objective bar | `#F2F2F2` on dark | `#000000` on light |

Motifs:
- **Ringed circle:** a colour disc with a black ring and an outer ring in the same colour. Used for step estimates (coloured by status), map status totals and avatars.
- **Tricolour stripe** under the nav: green 16%, amber 49%, red 35%.
- **Colour-blocked release bands** with large uppercase names.
- Soft corners: 8px on buttons and fields, 10–12px on cards, menus and toasts; pills (personas, step keys) fully round. Buttons are sentence case and bold: solid amber for the main action, otherwise a quiet outline (a 1px border and a faint tint of the text colour, a little stronger on hover). Fields and floating controls have a 1px soft border (`#C9C9CF` light, `#4A4A52` dark). Headings, labels and the nav stay uppercase. (Changed from square corners and uppercase 2px-outline buttons, which read as dated and too contrasty.)
- Status is never colour alone: every status also has a text label.

Type: bold uppercase grotesk headings. The site uses Akzidenz-Grotesk Next, which is licensed; use self-hosted Archivo (OFL) at 600/700/800, with `word-spacing: .14em` on uppercase text.

## Features

Status: **in** (v1), **later**, **out**, **?** (undecided).

### Tier 0 — the map
| Feature | Status |
|---|---|
| Backbone of objectives and activities, with steps under activities (see below) | in |
| Release slices | in |
| Drag and drop across cells and releases | in |
| Step details: title, markdown description, status, assignee | in |
| Workspaces → Maps, members, roles | in |

### Backbone hierarchy

Every map has the same three levels (decided after M1, replacing the choice of 3 or 4):

| Level | What it is | Jira Data Center (future sync) |
|---|---|---|
| Objective | Top of the backbone; a colour and the personas it serves | Initiative (Advanced Roadmaps) |
| Activity | One or more under each objective; a colour | Epic |
| Step | A card under an activity, in a release: key (`PREFIX-n`), title, description, status, assignee, estimate | Story |

Objectives and activities are one ordered tree of nodes with a `level`. Steps are separate rows placed in an (activity, release) cell. Each node and step carries a nullable external reference (system, key, URL), so sync can be added later without a migration that changes the data's shape.

### Tier 1 — planning essentials
| Feature | Status |
|---|---|
| Estimates with roll-ups | in |
| Personas linked to objectives | in |
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
| Linked maps (step in several maps) | in |
| Roadmap view synced with map | in |
| Custom status workflows | in |
| Comment-only viewer role | in |
| Saved, shareable filtered views | in |
| Attachments (steps, objectives, activities, comments; inline images) | done (M5) |
| Objective and activity details: description, files, comments and their own history; opened by clicking the card | done |
| Figma embeds, link attachments | later |
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
- **Steps are separate from where they sit on a map.** A `placements` table (step, map, activity, release, rank) lets one step appear in several maps (linked maps) without copying it.
- **Statuses are data, not an enum,** so custom workflows and Jira status mapping need no schema change.

## Milestones (proposed)

| # | Milestone | Contents |
|---|---|---|
| M0 | Foundations | Repo layout, Go server, migrations, docker-compose (Postgres, Keycloak, Mailpit), local + OIDC auth, workspaces and roles (incl. comment-only), CI, Helm skeleton |
| M1 | The map | Backbone of 3 or 4 levels, releases, stories, drag and drop, story details, custom statuses, event log, real-time sync and presence, audit history |
| M2 | Planning | Estimates and roll-ups, personas, labels, comments and @mentions, search, filter and highlight, saved views, zoom/collapse/focus |
| M3 | Sharing | Public share links, PDF/PNG export, roadmap view |
| M4 | Connected | Dependencies, linked maps, REST API with tokens and OpenAPI, webhooks, Mattermost (on hold) |
| M5 | Attachments | Files on steps, objectives, activities and comments, stored in Postgres (10 MB a file, any type); inline images in descriptions and comments; counts on cards (`docs/M5.md`) |
| Later | | Jira Data Center sync, Figma and link attachments, other integrations |
