# Changelog

Releases are tags `vX.Y.Z` on `main`. Each one publishes the image `ghcr.io/jrmagta/oh-my-story:X.Y.Z` (also `:X.Y` and `:latest`) and the chart `oci://ghcr.io/jrmagta/charts/oh-my-story` version `X.Y.Z` (`.github/workflows/release.yml`). The tag sets the chart's version and appVersion, so `Chart.yaml` is not bumped by hand. There are no GitHub Release pages; this file is the record.

## v0.2.0 — 2026-11-22

Attachments (M5), details for objectives and activities, and a softer look.

**Attachments** (`docs/M5.md`, #51–#54, #56, #57)
- Files on steps, objectives, activities and comments, stored in Postgres. Up to 10 MB a file (`OMS_ATTACHMENT_MAX_MB`, chart value `attachments.maxMB`); any type. The type is sniffed from the bytes, and only PNG, JPEG, GIF and WebP are shown in the browser; everything else downloads.
- Drop files on a panel or use Add files: per-file progress, a list with thumbnails, a full-size image viewer, and delete with Undo. Cards show a paperclip count.
- Images pasted or dropped into a description or comment upload and appear inline (`![name](attachment:<id>)`). Pasted screenshots are named `Pasted image <date time>.png`.
- Files on a comment being written are drafts, visible only to their uploader, until the comment is posted. A sweeper in each server deletes drafts older than a day and purges files deleted more than 30 days ago.
- Share links show the files and inline images of their own map, without uploader names.
- History, live updates and webhooks (`attachment.create`, `.delete`, `.restore`) carry file metadata, never the bytes.
- Deploy: the chart sets an ingress-nginx `proxy-body-size` at twice the limit; the LAN Caddyfile caps bodies at 20 MB. `docs/DEPLOY-ARGOCD.md` covers database size, cleanup, backups and memory.

**Objectives and activities** (#56, #58, #59)
- Clicking an objective or activity opens a details panel, like a step's (`?node=` in the URL): title, Markdown description, files, comments and its own history.
- Comments on objectives and activities work like comments on steps: threads, replies, resolve, @mentions with email, and files. Counts on a card are its own, never rolled up from its steps.
- A card's history is its own changes, files and comments. A step's history now includes its comments too.
- Webhook consumers: a `comment.*` event on an objective or activity has `nodeId` set and `stepId` null.

**Map and look** (#50, #55)
- Step cards take a colour, defaulting to their activity's.
- Dependencies show as a small mark on the card that toggles that step's lines.
- Labels have no colour: plain grey chips.
- Softer UI: rounded buttons and fields, 1px borders, sentence-case outline buttons that tint on hover, a rounded toast and a narrower estimate field (`docs/PLAN.md`, Design).

**Upgrading:** migrations `0015` to `0019` run in the migration Job as usual. Behind a proxy other than the chart's ingress-nginx, allow request bodies of at least the file limit plus 1 MB.

## v0.1.0 — 2026-11-22

The first tagged release: M0 to M4 (see `docs/M0.md` to `docs/M4.md`), without M4's Mattermost phase.
