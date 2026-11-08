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
