# 0002. SQLite as the database

Status: accepted

## Context
Single-node deployment on a Pi 4, a single admin writer, modest read traffic, and a desire for simple backups.

## Decision
Use SQLite in WAL mode with a pure-Go driver (`modernc.org/sqlite`, no CGO, easy arm64 cross-compile), `sqlc` for queries, goose for migrations and FTS5 for search.

## Consequences
- No database server to run, patch or secure; backup is a file snapshot (`.backup`).
- Single-writer limits; fine for this workload.
- Moving to Postgres later is possible because access goes through `sqlc`, but SQLite-specific features (FTS5) would need rework.
