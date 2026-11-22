# oh-my-story

Self-hosted user story mapping tool. The nav logo reads "Oh My Story". Features modelled on Avion; look and feel inspired by blink182.com (see the Design section of `docs/PLAN.md`; mock-up of record `docs/mockup/map-v3.html`, light and dark themes).

`docs/PLAN.md` is the source of truth for stack decisions, design tokens, feature scope (in / later / out), the backbone hierarchy and Jira mapping, and milestones. Update it when a decision changes.

`~/git-repos/featmap` is a Featmap fork kept as reference only (Go + React story mapper). Don't build on its code.

## Status

M0 to M5 done (see `docs/M0.md` to `docs/M5.md`; API contract in `docs/API.md` and `api/openapi.yaml`), except M4's Mattermost phase, which is on hold while the user decides whether they need it (its plan is in `docs/M4.md`; do not start it unasked). Along the way: map navigation (zoom, pan, minimap), Keycloak users (names from Keycloak, Add from Keycloak in Members; `docs/SSO.md`), and a LAN stack with HTTPS, its own Keycloak and certificate sign-in (`docs/LAN.md`).

M5 is attachments (`docs/M5.md`): files on steps, objectives, activities and comments, stored in Postgres (`attachments` and `attachment_data`, 10 MB a file via `OMS_ATTACHMENT_MAX_MB` / the chart's `attachments.maxMB`, any type, only raster images served inline). Images pasted or dropped into descriptions and comments are saved as `![name](attachment:<id>)`, which the web app resolves to the workspace or share-link URL. Files for a comment being written are drafts until posted; a sweeper in each server deletes stale drafts and purges files deleted over 30 days ago. Share links can read the files of their own map.

Since M5:
- Step cards take their own colour (default: their activity's), set like objective and activity colours.
- Dependencies show as a small mark on the card that toggles that step's lines, instead of a "Blocked by" label.
- Labels have no colour (grey chips).
- The UI is softer: rounded buttons and fields, 1px borders, and sentence-case outline buttons. The design rule in `docs/PLAN.md` was changed to match.
- Objectives and activities open a details panel when clicked, as steps do (`?node=` in the URL). It has a description, files, comments and its own history; counts show on the card. Comments belong to a step or a node.
- Step history includes the step's comments.

Next: whatever the user picks; `docs/PLAN.md` has only a "Later" row (Jira Data Center sync, Figma and link attachments, other integrations).

The map model is objectives › activities › steps. Objectives and activities are backbone `nodes` (title, description, colour; files, comments and history like steps); steps are cards (key, title, description, status, assignee, estimate, colour) placed in an (activity, release) cell. M0/M1 docs predate this and call steps "stories" (see the remodel note in `docs/M2.md`).

Dev machine: Go is at `~/.local/go/bin` and Node at `~/.nvm/versions/node/v24.14.0/bin`, neither on PATH (pass `GO=~/.local/go/bin/go` to make, and prepend the Node directory to PATH for `make build`/`make e2e`). Containers run on podman. `make up` starts the dev stack; `make lan LAN_HOST=jr-pc1.jrmagta.home` starts a separate try-out stack (or `make lan-services` once, then `make lan-app` to rebuild just the app) (`deploy/lan`, see `docs/LAN.md`): HTTPS on 127.0.0.1:8443 (8090 redirects), its own Keycloak with certificate sign-in on 127.0.0.1:18444 (Windows forwards LAN port 8444 there), Mailpit on 127.0.0.1:8026, and a test PKI in `~/.local/share/oh-my-story/lan-pki` (shared by all checkouts, not in git); Windows forwards ports 80, 443 and 8444 for the home network; `make test` and `make e2e` need it running. Playwright's Chromium needs `libgbm`/`libdrm`, which this machine lacks: run e2e with `LD_LIBRARY_PATH` pointing at extracted copies of those libs (`dnf download mesa-libgbm libdrm`, then `rpm2archive - < x.rpm | tar xz` in an empty directory; there is no `cpio`), or install them (`sudo dnf install mesa-libgbm libdrm`).
