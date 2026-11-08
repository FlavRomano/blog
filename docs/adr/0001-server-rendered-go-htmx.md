# 0001. Single Go binary with server-rendered templates and htmx

Status: accepted

## Context
The site is content-first, must be SEO-friendly, and runs on a Raspberry Pi 4. The repo started with separate `be/` and `fe/` folders. The owner wants something simple and fresh that also shows strong Go skills on a CV.

## Decision
Render HTML on the server with `html/template`, use htmx for interactivity and Tailwind (compiled at build time) for styling. Ship one static `linux/arm64` binary with embedded assets. Drop `fe/`.

## Consequences
- Minimal runtime footprint and one deploy artifact; no Node on the Pi.
- Excellent SEO and first paint without SSR complexity.
- Less "framework" keyword weight on the CV than React/Next; accepted.
- Rich client-side interactions would need Alpine/vanilla JS or a revisit of this decision.
