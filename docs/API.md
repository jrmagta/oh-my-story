# oh-my-story — HTTP API (M0: auth and workspaces)

JSON over HTTP. The web app and the tests are the callers. Map endpoints arrive in M1.

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
| Edit maps and stories (M1) | editor |
| Invite, change roles, remove members, change settings | admin |
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
