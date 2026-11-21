# Single sign-on with an existing Keycloak

oh-my-story signs people in with any OpenID Connect provider. It is not tied to a particular Keycloak: you point it at a realm with environment variables. With Keycloak you can also let admins add people from the realm in Members, without invites.

## 1. Create the client in your realm

In the Keycloak admin console, in the realm your people use:

1. **Clients → Create client.** Client type *OpenID Connect*, client ID e.g. `oh-my-story`.
2. **Capability config.** Turn on *Client authentication*. Keep *Standard flow*. For Members' "Add from Keycloak", also turn on *Service accounts roles*.
3. **Login settings.** *Valid redirect URIs*: `<OMS_BASE_URL>/auth/oidc/callback`, e.g. `http://jr-pc1.jrmagta.home/auth/oidc/callback`.
4. **Credentials.** Copy the client secret.

For "Add from Keycloak" (optional):

5. **Service account roles → Assign role → Client roles**: `realm-management` › `view-users` and `query-users`. These only read users.
6. **Client scopes → Add client scope**: `kc-admin-api`, as *Default*. Keycloak 26.8 and later need it for their admin API to accept the service account's token; older versions do not have it and do not need it.

People need an email address in Keycloak, and it must be verified to sign in for the first time unless an admin added them from Members.

## 2. Configure oh-my-story

| Variable | Example | Use |
|---|---|---|
| `OMS_OIDC_ISSUER` | `https://sso.example.com/realms/acme` | The realm's issuer, as it appears in tokens. Setting it turns SSO on |
| `OMS_OIDC_CLIENT_ID` | `oh-my-story` | |
| `OMS_OIDC_CLIENT_SECRET` | | From step 4 |
| `OMS_OIDC_NAME` | `Keycloak` | The sign-in button says "Sign in with …" |
| `OMS_OIDC_DIRECTORY` | `keycloak` | Optional: turns on "Add from Keycloak" in Members |
| `OMS_OIDC_DISCOVERY_URL` | `http://keycloak:8080/realms/acme` | Only when the server reaches Keycloak at a different address than browsers do (as in the dev compose stack) |

The LAN try-out stack runs its own Keycloak, set up this way with certificate sign-in as well (see `docs/LAN.md`). The Helm chart takes them as `oidc.issuer`, `oidc.clientID`, `oidc.name`, `oidc.directory` and `oidc.discoveryURL`, with the secret in the chart's secret under `oidc-client-secret`.

## How accounts work

- **Signing up stays invite-only.** Someone in the realm can sign in once they have an open invite, or once an admin has added them from Members.
- **Add from Keycloak** (Members, admins): search the realm by name, username or email, pick a role, and add. The person is a member at once, named and emailed as in Keycloak, and can be assigned work before they ever sign in. No email is sent. Owners cannot be added this way, as with invites.
- **Keycloak is the source of names and email** for people who sign in with it: first and last name and email are refreshed from Keycloak at every sign-in. An email another account already uses is left as it was.
- An account deactivated in oh-my-story stays deactivated, whatever Keycloak says.
