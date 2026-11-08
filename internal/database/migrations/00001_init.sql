-- +goose Up
CREATE TABLE posts (
    id           INTEGER PRIMARY KEY,
    slug         TEXT    NOT NULL UNIQUE,
    title        TEXT    NOT NULL,
    summary      TEXT    NOT NULL DEFAULT '',
    body_md      TEXT    NOT NULL,
    status       TEXT    NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'scheduled')),
    published_at TEXT,
    created_at   TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at   TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX posts_status_published_at ON posts (status, published_at DESC);

CREATE TABLE tags (
    id   INTEGER PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL
);

CREATE TABLE post_tags (
    post_id INTEGER NOT NULL REFERENCES posts (id) ON DELETE CASCADE,
    tag_id  INTEGER NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
    PRIMARY KEY (post_id, tag_id)
);
CREATE INDEX post_tags_tag_id ON post_tags (tag_id);

CREATE VIRTUAL TABLE posts_fts USING fts5(
    title, summary, body_md,
    content = 'posts', content_rowid = 'id'
);

-- +goose StatementBegin
CREATE TRIGGER posts_ai AFTER INSERT ON posts BEGIN
    INSERT INTO posts_fts (rowid, title, summary, body_md) VALUES (new.id, new.title, new.summary, new.body_md);
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER posts_ad AFTER DELETE ON posts BEGIN
    INSERT INTO posts_fts (posts_fts, rowid, title, summary, body_md) VALUES ('delete', old.id, old.title, old.summary, old.body_md);
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER posts_au AFTER UPDATE ON posts BEGIN
    INSERT INTO posts_fts (posts_fts, rowid, title, summary, body_md) VALUES ('delete', old.id, old.title, old.summary, old.body_md);
    INSERT INTO posts_fts (rowid, title, summary, body_md) VALUES (new.id, new.title, new.summary, new.body_md);
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER posts_au;
DROP TRIGGER posts_ad;
DROP TRIGGER posts_ai;
DROP TABLE posts_fts;
DROP TABLE post_tags;
DROP TABLE tags;
DROP TABLE posts;
