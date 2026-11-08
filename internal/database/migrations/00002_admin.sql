-- +goose Up
CREATE TABLE admin (
    id              INTEGER PRIMARY KEY CHECK (id = 1),
    password_hash   TEXT    NOT NULL,
    totp_secret_enc BLOB    NOT NULL,
    totp_last_step  INTEGER NOT NULL DEFAULT 0,
    created_at      TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at      TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE recovery_codes (
    id        INTEGER PRIMARY KEY,
    code_hash TEXT NOT NULL UNIQUE,
    used_at   TEXT
);

CREATE TABLE sessions (
    id           INTEGER PRIMARY KEY,
    token_hash   TEXT NOT NULL UNIQUE,
    csrf_token   TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    expires_at   TEXT NOT NULL
);

CREATE TABLE login_failures (
    id      INTEGER PRIMARY KEY,
    ip_hash TEXT NOT NULL,
    at      TEXT NOT NULL
);
CREATE INDEX login_failures_ip_at ON login_failures (ip_hash, at);
CREATE INDEX login_failures_at ON login_failures (at);

-- +goose Down
DROP TABLE login_failures;
DROP TABLE sessions;
DROP TABLE recovery_codes;
DROP TABLE admin;
