# Deploying to Rancher with ArgoCD: handoff brief

A brief for an agent working in the target environment. It describes what the Helm chart in `deploy/helm/oh-my-story` does today, what the target needs, and the changes to make. The changes are made in the target environment, not in this repository.

## Target

- A Rancher-managed Kubernetes cluster (likely RKE2). Apps are deployed by ArgoCD from a git repository.
- oh-my-story (OMS) goes into a namespace of its own, reached through an Ingress.
- Postgres and Keycloak already exist and are not deployed by the chart.
- Secrets live in a `secrets.yaml` encrypted with SOPS, which the Argo setup decrypts.

**Before changing anything, look at an existing app in the deployments repo** and copy its conventions: how the `Application` is written (single or multi-source, chart from git path or Helm/OCI repo), how `secrets.yaml` is wired (see [Secrets](#secrets-with-sops)), the ingress class and TLS annotations, the image registry, and whether namespaces belong to a Rancher project.

## The app in brief

- One Go binary in a distroless image (`gcr.io/distroless/static-debian12:nonroot`, uid 65532). `serve` runs the web app on :8080; `migrate` applies the database migrations and exits.
- It is configured only through environment variables, read in `internal/config/config.go`:

| Variable | Required | Notes |
|---|---|---|
| `OMS_DATABASE_URL` | yes | `postgres://` URL. Secret |
| `OMS_BASE_URL` | yes | The public origin, e.g. `https://story.example.com`. Must exactly match what browsers use: it sets Secure cookies, the Origin check on writes, the websocket Origin check and the OIDC redirect |
| `OMS_HTTP_ADDR` | no | Default `:8080` |
| `OMS_OIDC_ISSUER` | no | Setting it turns SSO on |
| `OMS_OIDC_CLIENT_ID`, `OMS_OIDC_CLIENT_SECRET` | with issuer | The client secret is a secret |
| `OMS_OIDC_DISCOVERY_URL` | no | Only when the pods reach Keycloak at a different URL than browsers do |
| `OMS_OIDC_NAME` | no | Label on the sign-in button |
| `OMS_OIDC_DIRECTORY` | no | `keycloak` turns on "Add from Keycloak" in Members |
| `OMS_SMTP_ADDR`, `OMS_SMTP_USERNAME`, `OMS_SMTP_PASSWORD`, `OMS_MAIL_FROM` | no | No `OMS_SMTP_ADDR`: emails are logged, not sent. The password is a secret |
| `OMS_WEBHOOK_BLOCK_PRIVATE` | no | `true` also stops webhooks posting to private addresses. **The chart does not expose it yet** |

- `/healthz` pings Postgres. The chart uses it for readiness and a TCP check for liveness, so a database outage takes pods out of the Service without restarting them.
- Replicas share nothing but Postgres. Live map updates and presence go through Postgres `LISTEN/NOTIFY`, so several replicas and the HPA work without sticky sessions. **This needs a direct or session-mode connection: a PgBouncer in transaction mode breaks `LISTEN`.**
- Live updates are websockets at `/api/workspaces/{slug}/maps/{id}/live`. The server pings every 25s, so default ingress idle timeouts (60s) are fine. The ingress controller must allow websocket upgrades (nginx and Traefik do by default).
- **The first person to open a fresh install becomes the admin.** Open it yourself straight after the first deploy, or keep the Ingress unreachable until you have.
- Keycloak client setup (redirect URI `<OMS_BASE_URL>/auth/oidc/callback`, the service account roles and the `kc-admin-api` scope) is in `docs/SSO.md`.

## The chart today

`deploy/helm/oh-my-story`, chart version 0.1.0. Templates: Deployment, Service, Ingress, HPA, migration Job, Secret, and an optional CloudNativePG `Cluster`. Values are documented inline in `values.yaml`; `ci/external-values.yaml` is a worked example for an external database.

Already fits the target:

- **Namespace.** No template sets `metadata.namespace`, so resources go to the Argo destination namespace.
- **External Postgres.** `database.url`, or the key `database-url` in `existingSecret`. Keep `cloudnativepg.enabled: false`.
- **Existing Keycloak.** `oidc.issuer`, `oidc.clientID`, `oidc.clientSecret` (or `oidc-client-secret` in `existingSecret`), `oidc.name`, `oidc.directory`, `oidc.discoveryURL`.
- **Ingress.** `ingress.enabled`, `className`, `annotations`, `host` (defaults to the host of `baseURL`), `tls.enabled`, `tls.secretName` (defaults to `<fullname>-tls`).
- **Pod security.** Pods meet the "restricted" Pod Security Standard (non-root, seccomp RuntimeDefault, all capabilities dropped, no privilege escalation, read-only root filesystem).

How Argo treats the Helm hooks (Argo renders the chart and applies it; it never runs `helm install`, and it converts Helm hook annotations into Argo hooks):

| Resource | Helm annotation | Under Argo |
|---|---|---|
| Secret (only when the chart makes it) | `pre-install,pre-upgrade`, weight -10, `before-hook-creation` | PreSync hook: deleted and recreated on every sync, not tracked as part of the app, left behind when the app is deleted |
| Migration Job, external DB | `pre-install,pre-upgrade`, weight 0, `before-hook-creation,hook-succeeded` | PreSync hook after the Secret: migrations run before each rollout. Correct |
| Migration Job, CloudNativePG | `post-install,pre-upgrade` | Mixed phases; unclear under Argo. Don't use CloudNativePG mode in this environment |

Gaps:

1. **No published image.** `image.repository` defaults to `ghcr.io/jrmagta/oh-my-story`, but nothing publishes it: this repository's CI only builds an image to load into kind.
2. **No way to trust a private CA.** There are no volume, CA or extra-env values. A Keycloak or Postgres with an internal CA fails TLS verification.
3. **Secrets from SOPS** need the wiring in [Secrets with SOPS](#secrets-with-sops). With `existingSecret` there is a first-sync ordering problem.
4. `OMS_WEBHOOK_BLOCK_PRIVATE`, extra env, extra pod labels and a pod-level `serviceAccountName` are not exposed.

## Changes to make

Work through these in order. Each one ends with a check.

### 1. Image

Get the image into a registry the cluster can pull from.

- If the cluster can reach ghcr.io, add a workflow in the OMS repository that builds `Dockerfile` and pushes `ghcr.io/<owner>/oh-my-story:<version>` on tags. Otherwise build it in the target environment and push it to the internal registry (Harbor or similar). The build pulls `node:24-alpine`, `golang:1.26-alpine` and `gcr.io/distroless/static-debian12:nonroot`; use mirrors if the build host is air-gapped.
- Pin a tag in values (`image.tag`); it defaults to the chart's `appVersion`, `0.1.0`.
- Private registry: put a pull secret in `secrets.yaml` (or create it the way other apps do) and list it in `imagePullSecrets`.

Check: `kubectl -n <ns> run oms-pull-test --rm -it --restart=Never --image=<image> -- healthcheck` pulls the image. (The command itself fails without a database; you are only checking the pull.)

### 2. Private CA (skip if Postgres and Keycloak have public certificates)

Go reads `SSL_CERT_DIR` and loads every PEM file in it, *in addition to* the image's default bundle at `/etc/ssl/certs/ca-certificates.crt`. pgx with `sslmode=verify-full` and no `sslrootcert` uses the same system roots, so one directory covers both Keycloak and Postgres. The LAN stack does the same thing (`deploy/lan/docker-compose.yml`, `SSL_CERT_DIR: /etc/oms-ca`).

Add to the chart:

```yaml
# values.yaml
caBundle:
  # A ConfigMap (or Secret) holding PEM CA certificates, in the release namespace.
  existingConfigMap: ""
  existingSecret: ""
```

When either is set, in both the Deployment and the migration Job:

- a volume from that ConfigMap or Secret, mounted read-only at `/etc/oms-ca`;
- `SSL_CERT_DIR=/etc/oms-ca` in the container env (add it to the `oms.env` helper in `templates/_helpers.tpl`, which both pods include).

If Rancher distributes the internal CA cluster-wide (for example trust-manager `Bundle`s), point `existingConfigMap` at that.

Then set `?sslmode=verify-full` in the database URL.

Check: the migration Job succeeds with `verify-full`. After sign-in through Keycloak, the app logs have no `x509: certificate signed by unknown authority`.

### 3. Secrets with SOPS

Find out which of these the deployments repo uses, from an existing app:

**A. helm-secrets** (the `Application` lists something like `secrets://secrets.yaml` under `helm.valueFiles`, and the repo-server has the helm-secrets plugin). `secrets.yaml` is a values file:

```yaml
database:
  url: postgres://oms:<password>@<host>:5432/oms?sslmode=verify-full   # URL-encode special characters in the password
oidc:
  clientSecret: <from Keycloak>
smtp:
  password: <optional>
```

Leave `existingSecret` empty and the chart makes the Secret. This works today. The cost is the hook behaviour in the table above: Argo recreates the Secret on every sync and doesn't delete it with the app. If you want it as an ordinary tracked resource, it still has to exist before the PreSync migration Job, so the migration has to move out of PreSync too (see B).

**B. KSOPS or a decrypted Secret manifest** (`secrets.yaml` is a `kind: Secret`). Give it the keys `database-url`, `oidc-client-secret` and optionally `smtp-password`, and set `existingSecret: <its name>`. **First-sync problem:** the migration Job is a PreSync hook, so it runs before the normal resources, Secret included, and fails on a fresh install. Pick one:

- annotate the Secret `argocd.argoproj.io/hook: PreSync`, `argocd.argoproj.io/hook-delete-policy: BeforeHookCreation` and `argocd.argoproj.io/sync-wave: "-1"`, so it is created before the Job; or
- add a chart value (e.g. `migration.argoSyncWave`) that renders the Job with `argocd.argoproj.io/sync-wave: "-1"` and no Helm hook annotations, and annotate the Secret `argocd.argoproj.io/sync-wave: "-2"`. Argo then applies the Secret, runs the Job and waits for it, then applies the Deployment (wave 0). A Job that is not a hook has an immutable spec, so give it a name that changes per image tag (e.g. `<fullname>-migrate-<tag>`) and set `ttlSecondsAfterFinished` or Argo's `Replace=true` sync option on it.

Either way, never commit plaintext values; `ci/external-values.yaml` contains dummy secrets for linting only.

Check: `kubectl -n <ns> get secret` shows the keys (not values), and the migration Job completes on a **fresh** namespace, not just on re-syncs.

### 4. Values and Application

Write a values file for the environment, kept with the deployments repo:

```yaml
image:
  repository: <registry>/oh-my-story
  tag: "<version>"
imagePullSecrets: []          # [{name: <pull-secret>}] for a private registry
baseURL: https://<oms-host>
existingSecret: ""            # route B: the Secret's name
database:
  url: ""                     # route A: in secrets.yaml
cloudnativepg:
  enabled: false
oidc:
  issuer: https://<keycloak-host>/realms/<realm>
  clientID: oh-my-story
  name: <label on the button>
  directory: keycloak         # optional
smtp:
  addr: <host:port>           # optional
  from: oh-my-story <no-reply@<domain>>
ingress:
  enabled: true
  className: <from `kubectl get ingressclass`>
  annotations: {}             # e.g. cert-manager.io/cluster-issuer: <issuer>
  tls:
    enabled: true
    secretName: ""            # defaults to <fullname>-tls
caBundle:                     # from step 2
  existingConfigMap: ""
replicaCount: 2
```

The `Application`, following the repo's conventions:

- `destination.namespace: <ns>`; `syncOptions: [CreateNamespace=true]` if Argo creates the namespace.
- A Rancher project: `managedNamespaceMetadata.annotations: {field.cattle.io/projectId: "<cluster-id>:<project-id>"}` (with `CreateNamespace=true`), or create the namespace in Rancher first.
- `helm.releaseName`: resource names are `<release>-oh-my-story`, or just `<release>` if the release name contains `oh-my-story`.
- If the namespace has NetworkPolicies, allow egress to Postgres, Keycloak, SMTP and DNS, and ingress from the ingress controller to port 8080.

Check: Argo shows the app Synced and Healthy, the migration Job completed, and `curl https://<oms-host>/healthz` returns 200.

### 5. Optional chart additions

Add only what the environment needs:

- `webhooks.blockPrivate` → `OMS_WEBHOOK_BLOCK_PRIVATE`.
- `extraEnv`, `extraVolumes` and `extraVolumeMounts`, applied to both the Deployment and the migration Job.
- `podLabels`, if NetworkPolicies or the mesh select on labels.

Keep `helm lint --strict` clean for every file in `ci/` and add a `ci/` values file for each new mode (for example `ci/argocd-values.yaml` with `caBundle` and `existingSecret` set).

## Acceptance

1. A fresh namespace syncs in one go: Secret, then migration, then Deployment, with no manual step.
2. `https://<oms-host>/` loads. The first visit creates the admin account; do it straight away.
3. "Sign in with <name>" round-trips through Keycloak (the realm client's redirect URI is `https://<oms-host>/auth/oidc/callback`).
4. Open the same map in two browsers: a change in one shows in the other within a second or two, and each shows the other as present. This checks the websocket through the ingress and `LISTEN/NOTIFY` across replicas.
5. Re-syncing with no changes leaves the app Synced, and a new image tag rolls out after its migration.
6. With `oidc.directory: keycloak`: Members › Add from Keycloak finds people in the realm.
