# Focus

*Rewritten each session. Not append-only — reflects current state only.*
*Overflow background and context → `FOCUS_context.md`. Scratch → `FOCUS_tmp.md`.*

---

## Active

- **wisp is live and counting** at https://wisp.mera.network (bh2).
  - **Reporting:** five products (cinderapps, offgridapp, eph-network,
    persona, wisp). The first closed day is 2026-10-01 (86 visitors).
  - **Dashboard views:** Issues · Last day so far (default) · Last day ·
    Last 3/7/30/90 days.
  - **Days close** 16 minutes after midnight UTC.
  - **Deploys** build the image locally and ship it with
    `docker save | docker load`; bh2 never builds.
- **uptime-wisp is live** on rbserver1
  (`http://rbserver1.lan:8080`, Basic Auth): 15 checks, alerts to
  ntfy `lnd_bh2_alerts_84af2f`. Edit
  `/opt/uptime-wisp/config/config.json`, then press "Reload config".

---

## Blocked

- Nothing blocking. The products' own deploy changes (image shipping)
  live in their repos and depend on their sessions.

- **Deferred:** the uptime-wisp heartbeat (the user checks the status
  page personally). `heartbeat.url` is ready if that changes.

---

## Next

1. **The products switch to image-shipping deploys** (the biggest bh2
   risk reducer): cinder A024, EphemNet A007, persona A011,
   offgrid A013. Until cinder's lands, its next server-side build
   re-pulls the 1.27 GB `golang:1.24-bookworm`.
2. **Make the Issues log survive restarts?** It's in memory and resets
   on every deploy, so problems from before a deploy disappear. It
   could persist to the stats DB; it holds no personal data.
3. **D002 leftovers:** confirm the proposed numbers (5-min flush,
   histogram buckets, batch limits) and decide on country/GeoIP
   (recommendation: leave it out).
4. **bh2 housekeeping (user's call):** cinder's stale `deploy-*` and
   `dev-*` images, ~200 MB.
