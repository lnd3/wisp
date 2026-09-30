# Reflection

Append-only log of learnings, gotchas, and patterns discovered during development.
Not session notes — these are stable insights worth carrying forward indefinitely.

Format: `YYYY-MM-DD | CATEGORY | insight`
Categories: GOTCHA · PATTERN · LEARNING · WARNING · DECISION · CONSTRAINT · FINDING

*If an entry needs more than one line, put the summary here and the detail in
`REFLECTION_extension.md` with a matching anchor. This file stays one-liners only.*

---

2026-09-30 | DECISION | wisp ingests only from product backends, never browsers (product-wide privacy guidelines) — aggregates preferred over unique events; the client-snippet design in D001 is superseded
2026-09-30 | GOTCHA | Debian's stock nginx http{} access_log and Caddy's http.log.error / reverse_proxy loggers all persist raw client IPs by default — wisp's deploy disables all three (verified the Caddy one empirically: a 502 logs remote_ip unless excluded); cinder's monitor.sh digest depends on IP-bearing access logs and must never be copied here
2026-09-30 | FINDING | persona's pinned Docker subnet 172.32.1.0/24 is outside RFC 1918 (172.16.0.0/12 ends at 172.31) — wisp uses 172.27.x instead; worth relaying to persona
2026-09-30 | WARNING | A plain hash of IPv4+User-Agent is brute-forceable (~4.3B addresses × a few thousand real UAs) — visitor keys must be HMACs under a product-held daily salt that wisp never sees, or the "no raw IP" invariant is false in practice
2026-09-30 | CONSTRAINT | Daily unique counts are not additive — summed over days they are visitor-days; the dashboard must never label a multi-day sum as "unique visitors" (D002)
2026-09-30 | DECISION | Backend-only ingest exists because forcing visitors to contact a third-party origin without consent is considered offensive, not just a privacy cost — recorded as C001; it also rules out first-party proxy scripts and browser-callable ingest (no CORS, no pixel)
2026-09-30 | GOTCHA | modernc.org/sqlite ≥ v1.40 requires Go ≥ 1.24 (v1.60 needs 1.26) — pinned v1.34.5 so the module builds with the local Go 1.23 toolchain; revisit when the dev toolchain moves to 1.24+
2026-09-30 | FINDING | Day close needs a seal step before summarizing: a batch validated just before the deadline can otherwise merge after the file is deleted, recreate it, and the next close's replace-upsert would overwrite the real day's stats with one late batch
