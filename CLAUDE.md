# oh-my-story

Self-hosted user story mapping tool. The nav logo reads "Oh My Story". Features modelled on Avion; look and feel inspired by blink182.com (see the Design section of `docs/PLAN.md`; mock-up of record `docs/mockup/map-v3.html`, light and dark themes).

`docs/PLAN.md` is the source of truth for stack decisions, design tokens, feature scope (in / later / out), the backbone hierarchy and Jira mapping, and milestones. Update it when a decision changes.

`~/git-repos/featmap` is a Featmap fork kept as reference only (Go + React story mapper). Don't build on its code.

## Status

M0 done (see `docs/M0.md`). M1 in progress: phase plan in `docs/M1.md`, API contract in `docs/API.md`. Next: phase 3, map UI.

Dev machine: Go is at `~/.local/go/bin` (not on PATH; pass `GO=~/.local/go/bin/go` to make). Containers run on podman. `make up` starts the stack; `make test` and `make e2e` need it running. Playwright's Chromium needs `libgbm`/`libdrm`, which this machine lacks: run e2e with `LD_LIBRARY_PATH` pointing at extracted copies of those libs, or install them (`sudo dnf install mesa-libgbm libdrm`).
