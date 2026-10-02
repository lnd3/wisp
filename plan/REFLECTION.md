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
2026-09-30 | GOTCHA | CSS display rules on an element override the HTML hidden attribute (.tip{display:flex} kept a hidden tooltip visible) — add [hidden]{display:none} for any styled toggled element; found only by rendering the dashboard, not by reading it
2026-10-01 | GOTCHA | wisp's products.json is shared server state written by several product sessions — a hand-written replacement wiped cinder's entry; register only via merge-only ops.sh register. Also: curl's UA is on the hook's bot list, so curl visits never count — test with a browser UA
2026-10-02 | WARNING | Never compile on bh2: its 8.7 GB disk is shared by every product. wisp's on-server golang build (~2 GB transient) filled it to 99%. Cross-compile locally and ship a binary; prune build cache in a trap so failures clean up too
2026-10-01 | DECISION | uptime-kuma rejected on size (867 MB slim image: Node, Azure/AWS SDKs, cloudflared) for our own Go prober (~25 MB image); its first live run found a real EphemNet DNS bug (ns1/ns2.mera.network NXDOMAIN), fixed the same day
2026-10-01 | GOTCHA | A deploy pre-flight must validate exactly as the service will run: uptime-wisp's `-once` check didn't require auth, so it would have approved a config the real service rejects (exit-2 restart loop). Hence `-check-config` with the real flags
2026-10-01 | GOTCHA | Single-file bind mounts miss editors that save by rename (new inode); mount the directory when a container must see edits live (uptime-wisp config/, a reload)
2026-10-02 | GOTCHA | Process matching on a remote host self-matched twice (pkill -f, an awk regex), because the pattern appears in the ssh command's own line. Match exact process names (pgrep -x docker)
2026-10-02 | FINDING | Tagged toolchain base images (golang:1.24-bookworm, 1.27 GB) survive both `docker image prune -f` and `docker builder prune`; server-side builds slowly eat a small shared disk even when they "clean up"

