# 0005. Privacy stance: minimal personal data

Status: accepted

## Context
The owner is in the EU (GDPR). Comments are unmoderated and need abuse controls, which tempts storing emails and raw IPs.

## Decision
- Commenters give a name only; no email is collected or stored.
- Client IP is read from `CF-Connecting-IP`, trusted only for requests arriving via the Cloudflare Tunnel, and stored only as a salted hash used for rate limiting and bans.
- Analytics use Cloudflare Web Analytics (cookieless); no in-app tracking.
- Turnstile is used for bot defense.

## Consequences
- Little personal data to protect, leak or delete on request; no cookie banner needed for analytics.
- No reply notifications or Gravatar.
- Bans on hashes are coarse (shared or rotating IPs) and require the salt to stay stable.
- Cloudflare becomes a dependency for the IP header, Turnstile and analytics.
- A short privacy page should describe this.
