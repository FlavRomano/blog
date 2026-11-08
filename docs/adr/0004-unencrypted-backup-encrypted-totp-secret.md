# 0004. Unencrypted Koofr backup, TOTP secret encrypted in the DB

Status: accepted

## Context

Nightly backups (SQLite snapshot + uploads) go to Koofr via WebDAV. The DB holds the admin password hash, the TOTP secret, recovery code hashes and IP hashes. The owner chose not to encrypt the backup files (simplicity, Koofr is private, protected by an app-specific password). Anyone with Koofr access would otherwise obtain the TOTP secret, defeating 2FA on a public `/admin`.

## Decision

- Do not encrypt backup files.
- Encrypt the TOTP secret (and recovery code material) inside the DB with a key stored only in the Pi's environment file, never in the DB or the backup.
- Use an app-specific Koofr password with a `chmod 600` rclone config.

## Consequences

- A leaked backup exposes content, password hash and IP hashes, but not a usable TOTP secret.
- The Argon2 password hash is still offline-crackable; use a strong password.
- Losing the env key means TOTP must be re-enrolled via the CLI reset; the restore procedure must document this.
- Revisit option: wrap backups with `age` if more sensitive data is ever stored.
