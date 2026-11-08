# Blog

Personal site and blog: a single Go binary with server-rendered templates, htmx and SQLite.
Design notes live in `docs/` (spec and ADRs).

## Develop
    make run     # serves on 127.0.0.1:8080, data in ./data
    make test
    make lint
    make sqlc    # regenerate after editing internal/database/{migrations,queries}
    make build-arm64

Configuration is via `BLOG_*` environment variables (see `internal/config`).

## Admin

The admin area (`/admin`) is disabled unless `BLOG_TOTP_KEY` is set. Credentials are
created only from the CLI, never through the web.

```sh
export BLOG_TOTP_KEY=$(openssl rand -hex 32)   # keep stable: losing it means re-enrolling
export BLOG_IP_SALT=$(openssl rand -hex 32)    # keep stable: rotating it resets IP bans
export BLOG_TIMEZONE=Europe/Rome               # used for "publish at" times

go run ./cmd/blog admin setup   # password + prints a QR code, secret and recovery codes
go run ./cmd/blog admin reset   # re-enroll (invalidates all sessions)
```

Sign in with the password plus a TOTP (or one-time recovery) code. Uploaded images
live in `BLOG_UPLOADS_DIR` and are served as-is (EXIF is not stripped).

## Comments

Anonymous, unmoderated, name-only, plain text, nested up to 4 levels. Enabled when
`BLOG_IP_SALT` is set. Client IPs are stored only as salted hashes (rate limits: 3 per
10 min and 20 per day per IP; bans are per hash, managed at `/admin/comments`).

| Variable | Purpose |
| --- | --- |
| `BLOG_TURNSTILE_SITE_KEY`, `BLOG_TURNSTILE_SECRET` | Cloudflare Turnstile bot check (set both, or neither to skip it in dev) |
| `BLOG_TRUST_TUNNEL=true` | Trust `CF-Connecting-IP` (only from the local tunnel connection); required in production |
| `BLOG_NOTIFY_URL` | Optional ntfy-style webhook; new comments are POSTed (name and an excerpt) to it |

The admin nav shows how many comments are new since you last opened the comments page.
