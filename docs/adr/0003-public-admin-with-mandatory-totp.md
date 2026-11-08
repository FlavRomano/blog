# 0003. Public `/admin` protected by mandatory TOTP

Status: accepted

## Context
The admin surface is the highest-value target: comments are public and unmoderated, and the host is on a home network. A Tailscale-only admin was the safer option, but the owner wants to manage the site from anywhere without a VPN.

## Decision
Expose `/admin` publicly, protected by an Argon2 password, **mandatory** TOTP, login rate limiting with lockout, CSRF protection and httpOnly session cookies. Recovery uses one-time recovery codes generated at setup, plus a CLI reset command on the Pi (SSH over Tailscale).

## Consequences
- Convenience, at the cost of a larger attack surface than a private admin.
- TOTP cannot be optional; the TOTP secret is encrypted at rest (ADR 0004).
- Revisit option: put `/admin` behind Cloudflare Access or Tailscale later without changing the app.
