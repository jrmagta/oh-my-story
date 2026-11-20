# oh-my-story — HTTP API

JSON over HTTP. The web app and the tests are the callers. Auth and workspaces arrived in M0; maps and workflows in M1 (see [Maps](#maps)).

## Conventions

- **Session:** cookie `oms_session`, `HttpOnly`, `SameSite=Lax`, `Secure` when `OMS_BASE_URL` is https. 30-day fixed lifetime. The server stores only the SHA-256 of the token. Signing in ends the session the browser held before and deletes the user's expired sessions.
- **CSRF:** every non-GET `/api` request must send `Content-Type: application/json`. If an `Origin` header is present it must equal `OMS_BASE_URL`, which the server normalises at startup to the form browsers send (lowercase, no default port). Browsers cannot send either cross-site without a CORS preflight, which the server never grants.
- **Errors:** `{"error": {"code": "<code>", "message": "<text>"}}`. Codes: `bad_request`, `unauthenticated`, `forbidden`, `not_found`, `conflict`, `gone`, `email_failed` (502).
- **Workspaces** are addressed by slug.

## Roles

Ordered `owner > admin > editor > commenter > viewer`. An action needs at least the role shown.

| Action | Minimum role |
|---|---|
| View workspace, list members | viewer |
| Comment (M2) | commenter |
| Edit maps and stories | editor |
| Invite, change roles, remove members, change settings, edit workflows and statuses | admin |
| Delete workspace | owner |

Extra rules:
- Only an owner can make someone an owner, or change or remove an owner.
- A workspace always keeps at least one owner. Demoting or removing the last owner, including leaving, returns `conflict`.
- Any member can leave a workspace (`DELETE` on their own membership).
- Invites cannot grant `owner`.

## Sign-up is invite-only

1. **Bootstrap:** while the `users` table is empty, `POST /api/setup` creates the first user. After that it returns `gone`.
2. **Invite:** an admin invites an email with a role. The email carries a link to `/invites/{token}` (7-day expiry, single use, one open invite per email per workspace).
3. **Accept:**
   - Signed in with the invited email: `POST /api/invites/{token}/accept` with `{}` adds the membership.
   - No account: the same call with `{displayName, password}` creates a local account, adds the membership and signs in. Holding the token proves control of the inbox.
   - Signed in with a different email: `forbidden`.
4. **SSO:** an OIDC login whose verified email matches an existing user links to that user. With no matching user, it creates one only if an open invite exists for that email; otherwise it is refused. The new user then accepts the invite as above. A deactivated user is refused even with an open invite.

## Endpoints

| Method and path | Auth | Body → response |
|---|---|---|
| `GET /api/setup` | none | → `{needsBootstrap}` |
| `POST /api/setup` | none | `{email, displayName, password}` → `201` user, sets session |
| `POST /api/auth/login` | none | `{email, password}` → user, sets session. Wrong email or password: `unauthenticated` (same message for both) |
| `POST /api/auth/logout` | session | → `204`, clears session |
| `GET /api/me` | session | → `{user, workspaces: [{slug, name, role}]}` |
| `GET /api/auth/providers` | none | → `{oidc: {enabled, name}}` |
| `GET /auth/oidc/login?next=<path>` | none | browser redirect to the identity provider |
| `GET /auth/oidc/callback` | none | browser redirect to `next` (a local path, default `/`) with a session, or to `/login?error=<code>&next=<path>`: `not_invited`, `account_disabled`, `sso_email_unverified`, `sso_denied`, `sso_expired`, `sso_state`, `sso_failed` |
| `GET /api/workspaces` | session | → my workspaces |
| `POST /api/workspaces` | session | `{name, slug}` → `201` workspace; caller becomes owner |
| `GET /api/workspaces/{slug}` | viewer | → workspace incl. `myRole` |
| `PATCH /api/workspaces/{slug}` | admin | `{name?, keyPrefix?, estimateUnit?}` → workspace |
| `DELETE /api/workspaces/{slug}` | owner | → `204` |
| `GET /api/workspaces/{slug}/members` | viewer | → `[{userId, email, displayName, role}]` |
| `PATCH /api/workspaces/{slug}/members/{userId}` | admin | `{role}` → member |
| `DELETE /api/workspaces/{slug}/members/{userId}` | admin, or self | → `204` |
| `GET /api/workspaces/{slug}/invites` | admin | → open invites |
| `POST /api/workspaces/{slug}/invites` | admin | `{email, role}` → `201` invite; sends the email. If sending fails: `email_failed`, and the invite is not kept |
| `DELETE /api/workspaces/{slug}/invites/{id}` | admin | → `204` |
| `GET /api/invites/{token}` | none | → `{workspaceName, email, role}`; expired or used: `gone` |
| `POST /api/invites/{token}/accept` | optional | `{}` or `{displayName, password}` → `{workspace}`, may set session |

Non-members get `not_found` for a workspace, so slugs do not leak.

Any other `/api/` path is a JSON `not_found`. Any other GET serves the web app (`index.html` for page routes, with a strict Content-Security-Policy and `Referrer-Policy: no-referrer`, since invite links carry their token in the path).

## Maps

All paths below start with `/api/workspaces/{slug}`. Reading needs viewer; map, node, release and story writes need editor; workflow and status writes need admin. Ids from another workspace, and malformed ids, return `not_found`. A rule the change breaks (a wrong parent level, an anchor that is not a sibling, a status from another workflow) returns `bad_request` with the reason.

Every write is logged in `events` in the same transaction (see `DATA_MODEL.md`). A write that changes nothing logs nothing.

**Positions.** Ordered things take `afterId` or `beforeId` (not both) to name the neighbour they go next to; with neither they go last. In a story cell the ids are story ids; elsewhere they are ids of the siblings themselves.

**Optional nulls.** Where noted, a field that is absent leaves the value alone and `null` clears it.

### Workflows and statuses

| Method and path | Body → response |
|---|---|
| `GET /workflows` | → `[{id, name, isDefault, statuses: [status]}]`, default first |
| `POST /workflows` | `{name}` → `201` workflow, with the standard four statuses |
| `PATCH /workflows/{id}` | `{name}` → workflow |
| `POST /workflows/{id}/statuses` | `{name, category, colour?, afterId?, beforeId?}` → `201` status. Duplicate name: `conflict` |
| `PATCH /statuses/{id}` | `{name?, category?, colour?}` → status |
| `POST /statuses/{id}/move` | `{afterId?, beforeId?}` → status |
| `DELETE /statuses/{id}?replacementId=<status>` | → `204`. Stories in the status move to the replacement, another status of the same workflow; required when any story uses it. A workflow keeps at least one status |

A status is `{id, workflowId, name, category, colour, rank}`; `category` is `todo`, `doing`, `done` or `blocked`.

### Maps, nodes, releases, stories

| Method and path | Body → response |
|---|---|
| `GET /maps` | → `[map]` |
| `POST /maps` | `{name, description?, levels, workflowId?}` → `201` map. `levels` is 3 or 4; the workflow defaults to the workspace default and cannot change later |
| `GET /maps/{mapId}` | → `{map, workflow, nodes, releases, stories, placements, lastEventId}`: the whole map, deleted items left out. `lastEventId` is the newest event included, for resuming the live feed |
| `PATCH /maps/{mapId}` | `{name?, description?, levels?}` → map. `levels` cannot change once the map has nodes |
| `DELETE /maps/{mapId}` | → `204` (soft delete) |
| `POST /maps/{mapId}/nodes` | `{level, title, parentId?, description?, colour?, afterId?, beforeId?}` → `201` node |
| `PATCH /maps/{mapId}/nodes/{id}` | `{title?, description?, colour?}` → node |
| `POST /maps/{mapId}/nodes/{id}/move` | `{parentId, afterId?, beforeId?}` → node. `parentId` is required, `null` for the top level; a node keeps its level |
| `DELETE /maps/{mapId}/nodes/{id}` | → `204`. Deletes the subtree and the stories on its steps |
| `POST /maps/{mapId}/nodes/{id}/restore` | `{}` → node. Restores what that delete removed; the parent must not be deleted |
| `POST /maps/{mapId}/releases` | `{name, description?, targetDate?, afterId?, beforeId?}` → `201` release |
| `PATCH /maps/{mapId}/releases/{id}` | `{name?, description?, targetDate?}` → release. `targetDate`: optional null |
| `POST /maps/{mapId}/releases/{id}/move` | `{afterId?, beforeId?}` → release |
| `DELETE /maps/{mapId}/releases/{id}` | → `204`. Its stories go to the end of their step's Unscheduled cell |
| `POST /maps/{mapId}/stories` | `{title, stepId, releaseId?, descriptionMd?, afterId?, beforeId?}` → `201 {story, placement}`. Gets the next workspace key and the workflow's first status |
| `PATCH /maps/{mapId}/stories/{id}` | `{title?, descriptionMd?, statusId?, assigneeId?}` → story. `assigneeId`: optional null; must be a member |
| `POST /maps/{mapId}/stories/{id}/move` | `{stepId, releaseId, version, afterId?, beforeId?}` → `{placement, stale}`. `releaseId` is required, `null` for Unscheduled. `version` is the story version the client showed; the move applies regardless and `stale` says whether someone changed the story since |
| `DELETE /maps/{mapId}/stories/{id}` | → `204` (soft delete) |
| `POST /maps/{mapId}/stories/{id}/restore` | `{}` → story. Its step must not be deleted |

Shapes: map `{id, workflowId, name, description, levels}`; node `{id, parentId, level, title, description, colour, rank, version}`; release `{id, name, description, targetDate, rank}` with `targetDate` as `YYYY-MM-DD`; story `{id, key, title, descriptionMd, statusId, assigneeId, version}`; placement `{id, storyId, stepId, releaseId, rank}`.

Limits: names 1 to 100 characters, titles 1 to 500, descriptions up to 50,000; colours are `#RRGGBB`.

### History

| Method and path | Response |
|---|---|
| `GET /maps/{mapId}/history?before=&limit=` | the map's events, newest first |
| `GET /maps/{mapId}/stories/{id}/history?before=&limit=` | the story's events and its placement moves, newest first; kept after the story is deleted |

Both need viewer and return `{entries: [event + actorName], next}`. `actorName` is `""` when the account no longer exists. `limit` is 1 to 200 (default 50); pass `next` as `before` for the following page; `next` is null on the last one.

### Live

`GET /maps/{mapId}/live?after=<lastEventId>` (viewer) upgrades to a WebSocket. `after` is the `lastEventId` of the map payload the client holds. The server sends JSON messages and never reads any:

| Message | Meaning |
|---|---|
| `{type: "event", event}` | One event after `after`, oldest first: first the backlog, then live ones. Ids only grow and none repeat |
| `{type: "presence", users: [{id, name}]}` | Everyone watching the map now, on any instance, each once, sorted by name. Sent whenever it changes |
| `{type: "resync"}` | The client is too far behind (over 1,000 events, or too slow to read): reload the map and reconnect from its new `lastEventId`. The socket then closes |

An event is `{id, mapId, actorId, entity, entityId, action, before, after, at}`. `entity` is `map`, `node`, `release`, `story`, `placement`, `workflow` or `status`; `action` is `create`, `update`, `move`, `delete` or `restore`. A create carries the whole entity in `after`; an update or move carries the changed fields in `before` and `after`, plus the new `version` where the entity has one; a placement move also carries the story's new version as `storyVersion`. A delete carries the fields that name the row in `before` (a node's `title` and `level`, a story's `key` and `title`, a release's, status's or map's `name`), a restore the same in `after`; a restore brings back rows the client no longer holds, so clients reload on it. Workspace-wide events (workflows and statuses, `mapId` null) go to every map in the workspace.

The upgrade needs the session cookie and an `Origin` equal to `OMS_BASE_URL`'s host (or none, for non-browser clients). The server pings every 25 seconds and closes the socket within a minute of the caller leaving the workspace.

## Validation

- Email: trimmed, must contain one `@`; stored case-insensitively.
- Password: 12 to 256 characters. Hashed with argon2id (m=19 MiB, t=2, p=1), PHC string format.
- Display name: 1 to 100 characters after trimming.
- Slug: `^[a-z0-9](?:[a-z0-9-]{0,38}[a-z0-9])?$`.
- Key prefix: `^[A-Z][A-Z0-9]{0,9}$`.

## Configuration added in phase 3

| Variable | Default | Use |
|---|---|---|
| `OMS_BASE_URL` | `http://localhost:8080` | links in emails, Origin check, Secure cookie |
| `OMS_SMTP_ADDR` | empty: emails are logged, not sent | `host:port` |
| `OMS_SMTP_USERNAME`, `OMS_SMTP_PASSWORD` | empty | PLAIN auth when set |
| `OMS_MAIL_FROM` | `oh-my-story <no-reply@localhost>` | sender |
| `OMS_OIDC_ISSUER` | empty: SSO off | issuer URL as it appears in tokens |
| `OMS_OIDC_DISCOVERY_URL` | the issuer | where the server fetches discovery (differs in compose) |
| `OMS_OIDC_CLIENT_ID`, `OMS_OIDC_CLIENT_SECRET` | — | required when the issuer is set |
| `OMS_OIDC_NAME` | `SSO` | button label |

## Deferred

Login rate limiting, password reset (M1 backlog: "Reset a forgotten password"), email change, session listing and revoke-all.
