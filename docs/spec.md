# Personal site & blog: Spec (v1)

Status: agreed. Decisions with lasting trade-offs live in [`adr/`](adr/).

## Goals
A public personal site/blog, self-hosted on a Raspberry Pi 4, with a single-user admin. It doubles as a CV showcase, so quality (tests, CI/CD, security, docs) is a feature.

## Actors
- **Visitor**: reads posts, searches, follows RSS, comments anonymously.
- **Admin**: the site owner, the only account.

## Stack
- Go (`net/http` or chi), server-rendered `html/template`, htmx, Tailwind (compiled at build time). See ADR 0001.
- SQLite (pure-Go driver), `sqlc`, goose migrations, FTS5 search. See ADR 0002.
- Single Go module in `blog/`; the separate `fe/` folder is dropped.
- English only.

## Public features
- Post list with pagination and tags (tags only, no categories).
- Post page: Markdown rendered to sanitized HTML, syntax highlighting, reading time, table of contents.
- About/CV and Projects pages.
- Search (FTS5), RSS/Atom, `sitemap.xml`, `robots.txt`, SEO/OpenGraph tags.
- Dark mode, responsive, accessible.
- Analytics via Cloudflare Web Analytics (no in-app view counts).

## Comments (no moderation)
- Commenter provides a name only. No email is collected or stored.
- Spam defense: Cloudflare Turnstile, honeypot field, per-IP rate limit, length and link limits, Markdown stripped/sanitized.
- Fully nested threads, **max depth 4**; replies deeper than 4 attach to the level-4 parent.
- Deleting a comment with replies leaves a `[deleted]` tombstone and keeps the replies.
- IP identity: `CF-Connecting-IP`, trusted only when the request comes from the tunnel; stored only as a salted hash. Admin can ban by hash. See ADR 0005.
- Admin gets a notification on new comments.

## Admin (`/admin`, public internet)
- Argon2 password, **mandatory TOTP**, login rate limiting and lockout, CSRF protection, httpOnly session cookie. See ADR 0003.
- TOTP secret encrypted at rest; the key lives only in the Pi's env file. See ADR 0004.
- Recovery: one-time recovery codes generated at setup, plus a CLI command on the Pi to reset credentials.
- Post CRUD: textarea Markdown editor with htmx live preview; states draft / published / scheduled; tags; image upload.
- Scheduled posts are published by an in-app ticker (every minute).
- Comment list with delete and an IP-hash ban list.
- Source of truth for content is the database, edited only via the admin UI.
- Images are stored as-is on local disk and served by the app.

## Operations
- Raspberry Pi 4 on an SSD (not an SD card) running the binary under systemd. See ADR 0006.
- Cloudflare Tunnel to a domain on Cloudflare DNS (domain still to be purchased).
- CI/CD: GitHub Actions builds `linux/arm64`, deploys over Tailscale SSH, restarts via systemd.
- Nightly backup to Koofr via WebDAV with rclone: SQLite `.backup` snapshot plus `uploads/`, 30-day retention, app-specific Koofr password, healthchecks.io dead-man ping. Backups are not encrypted (ADR 0004).
- A restore test is part of the definition of done.
- `/healthz`, structured logging (`slog`), security headers.

## Quality bar
Unit and integration tests, `golangci-lint`, README with architecture diagram and live demo link.

## Out of scope (v1)
Multi-user/roles, newsletters, i18n, social login, email collection, categories, image processing, external object storage.

## Milestones
1. Skeleton, DB, migrations, Markdown pipeline.
2. Public read pages, tags, RSS, SEO.
3. Admin auth (password + TOTP + recovery) and post CRUD, image upload, scheduling.
4. Comments, nesting, anti-spam, IP-hash bans.
5. Search, polish, CI/CD, Pi deployment, tunnel, backups and restore test.

## Defaults chosen for open items
Tags only; textarea + htmx live preview; SSD for the Pi. Revisit freely.
