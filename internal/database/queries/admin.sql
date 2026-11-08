-- name: GetAdmin :one
SELECT * FROM admin WHERE id = 1;

-- name: UpsertAdmin :exec
INSERT INTO admin (id, password_hash, totp_secret_enc, totp_last_step)
VALUES (1, ?, ?, 0)
ON CONFLICT (id) DO UPDATE SET
    password_hash = excluded.password_hash,
    totp_secret_enc = excluded.totp_secret_enc,
    totp_last_step = 0,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now');

-- name: AdvanceTOTPStep :execrows
UPDATE admin SET totp_last_step = sqlc.arg(step)
WHERE id = 1 AND totp_last_step < sqlc.arg(step);

-- name: DeleteRecoveryCodes :exec
DELETE FROM recovery_codes;

-- name: InsertRecoveryCode :exec
INSERT INTO recovery_codes (code_hash) VALUES (?);

-- name: UseRecoveryCode :execrows
UPDATE recovery_codes SET used_at = ?
WHERE code_hash = ? AND used_at IS NULL;

-- name: CountUnusedRecoveryCodes :one
SELECT count(*) FROM recovery_codes WHERE used_at IS NULL;

-- name: CreateSession :exec
INSERT INTO sessions (token_hash, csrf_token, created_at, last_seen_at, expires_at)
VALUES (?, ?, ?, ?, ?);

-- name: GetSessionByTokenHash :one
SELECT * FROM sessions WHERE token_hash = ?;

-- name: TouchSession :exec
UPDATE sessions SET last_seen_at = ? WHERE id = ?;

-- name: DeleteSessionByTokenHash :exec
DELETE FROM sessions WHERE token_hash = ?;

-- name: DeleteAllSessions :exec
DELETE FROM sessions;

-- name: DeleteStaleSessions :exec
DELETE FROM sessions WHERE expires_at <= ? OR last_seen_at <= ?;

-- name: RecordLoginFailure :exec
INSERT INTO login_failures (ip_hash, at) VALUES (?, ?);

-- name: CountLoginFailuresByIP :one
SELECT count(*) FROM login_failures WHERE ip_hash = ? AND at > ?;

-- name: CountLoginFailures :one
SELECT count(*) FROM login_failures WHERE at > ?;

-- name: ClearLoginFailuresByIP :exec
DELETE FROM login_failures WHERE ip_hash = ?;

-- name: ClearLoginFailures :exec
DELETE FROM login_failures;

-- name: PruneLoginFailures :exec
DELETE FROM login_failures WHERE at <= ?;
