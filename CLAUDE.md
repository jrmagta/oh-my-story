# oh-my-story

Self-hosted user story mapping tool. The nav logo reads "Oh My Story". Features modelled on Avion; look and feel inspired by blink182.com (see the Design section of `docs/PLAN.md`; mock-up of record `docs/mockup/map-v3.html`, light and dark themes).

`docs/PLAN.md` is the source of truth for stack decisions, design tokens, feature scope (in / later / out), the backbone hierarchy and Jira mapping, and milestones. Update it when a decision changes.

`~/git-repos/featmap` is a Featmap fork kept as reference only (Go + React story mapper). Don't build on its code.

## Status

M0 to M5 done (see `docs/M0.md` to `docs/M5.md`; M5 is attachments, stored in Postgres; API contract in `docs/API.md` and `api/openapi.yaml`), except M4's Mattermost phase, which is on hold while the user decides whether they need it (its plan is in `docs/M4.md`; do not start it unasked). Along the way: map navigation (zoom, pan, minimap), Keycloak users (names from Keycloak, Add from Keycloak in Members; `docs/SSO.md`), and a LAN stack with HTTPS, its own Keycloak and certificate sign-in (`docs/LAN.md`). Next: whatever the user picks; after M5, `docs/PLAN.md` has only a "Later" row (Jira Data Center sync, Figma and link attachments, other integrations).

The map model is objectives › activities › steps. Objectives and activities are backbone `nodes`; steps are cards (key, title, description, status, assignee, estimate) placed in an (activity, release) cell. M0/M1 docs predate this and call steps "stories" (see the remodel note in `docs/M2.md`).

Dev machine: Go is at `~/.local/go/bin` and Node at `~/.nvm/versions/node/v24.14.0/bin`, neither on PATH (pass `GO=~/.local/go/bin/go` to make, and prepend the Node directory to PATH for `make build`/`make e2e`). Containers run on podman. `make up` starts the dev stack; `make lan LAN_HOST=jr-pc1.jrmagta.home` starts a separate try-out stack (or `make lan-services` once, then `make lan-app` to rebuild just the app) (`deploy/lan`, see `docs/LAN.md`): HTTPS on 127.0.0.1:8443 (8090 redirects), its own Keycloak with certificate sign-in on 127.0.0.1:18444 (Windows forwards LAN port 8444 there), Mailpit on 127.0.0.1:8026, and a test PKI in `~/.local/share/oh-my-story/lan-pki` (shared by all checkouts, not in git); Windows forwards ports 80, 443 and 8444 for the home network; `make test` and `make e2e` need it running. Playwright's Chromium needs `libgbm`/`libdrm`, which this machine lacks: run e2e with `LD_LIBRARY_PATH` pointing at extracted copies of those libs (`dnf download mesa-libgbm libdrm`, then `rpm2archive - < x.rpm | tar xz` in an empty directory; there is no `cpio`), or install them (`sudo dnf install mesa-libgbm libdrm`).
