-- +goose Up
CREATE TABLE comments (
    id         INTEGER PRIMARY KEY,
    post_id    INTEGER NOT NULL REFERENCES posts (id) ON DELETE CASCADE,
    parent_id  INTEGER REFERENCES comments (id) ON DELETE CASCADE,
    depth      INTEGER NOT NULL CHECK (depth BETWEEN 1 AND 4),
    author     TEXT    NOT NULL,
    body       TEXT    NOT NULL,
    ip_hash    TEXT    NOT NULL DEFAULT '',
    deleted    INTEGER NOT NULL DEFAULT 0,
    seen       INTEGER NOT NULL DEFAULT 0,
    created_at TEXT    NOT NULL
);
CREATE INDEX comments_post ON comments (post_id, created_at, id);
CREATE INDEX comments_parent ON comments (parent_id);
CREATE INDEX comments_ip_at ON comments (ip_hash, created_at);
CREATE INDEX comments_at ON comments (created_at);

CREATE TABLE bans (
    ip_hash    TEXT PRIMARY KEY,
    created_at TEXT NOT NULL
);

-- +goose Down
DROP TABLE bans;
DROP TABLE comments;
