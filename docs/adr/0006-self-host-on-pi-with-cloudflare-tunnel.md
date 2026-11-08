# 0006. Self-host on a Raspberry Pi 4 behind a Cloudflare Tunnel

Status: accepted

## Context
The site is hosted at home on a Pi 4. A home IP may be dynamic or behind CGNAT, and port forwarding exposes the home network.

## Decision
- Run the static arm64 binary under systemd as an unprivileged user, on an SSD rather than an SD card.
- Publish through `cloudflared` (Cloudflare Tunnel) with a purchased domain on Cloudflare DNS; no inbound ports opened.
- Deploy from GitHub Actions over Tailscale SSH, then `systemctl restart`.
- Nightly rclone backup to Koofr over WebDAV (see ADR 0004), monitored by a healthchecks.io dead-man ping; the restore procedure is tested.
- Harden the Pi: SSH keys only, `ufw`, unattended upgrades.

## Consequences
- No public home IP, free HTTPS and DDoS protection.
- Uptime depends on home power and internet; accepted for a personal site.
- Requires buying a domain and moving its DNS to Cloudflare.
- Cloudflare, Tailscale, GitHub, Koofr and healthchecks.io are operational dependencies.
