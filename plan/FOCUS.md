# Focus

*Rewritten each session. Not append-only — reflects current state only.*
*Overflow background and context → `FOCUS_context.md`. Scratch → `FOCUS_tmp.md`.*

---

## Active

- **wisp is live and counting** at https://wisp.mera.network (bh2).
  - **Reporting:** five products (cinderapps, offgridapp, eph-network,
    persona, wisp). cinderapps now reports from bh3 (cinder moved its
    stack there, cinder A026) with no change on wisp's side.
  - **Dashboard views:** Issues · Last day so far (default) · Last day ·
    Last 3/7/30/90 days. Days close 16 minutes after midnight UTC.
  - **Deploys** build the image locally and ship it
    (`docker save | docker load`); servers never build.
- **uptime-wisp is live** on rbserver1 (`http://rbserver1.lan:8080`,
  Basic Auth): 19 checks, alerts to ntfy `lnd_bh2_alerts_84af2f`. Edit
  `/opt/uptime-wisp/config/config.json`, then press "Reload config".
  - **Connectivity gate (A001, done):** when all three independent
    anchors fail, rounds judge nothing and no alerts go out.
  - **Grouping:** three or more simultaneous changes arrive as one
    alert.
- **health-wisp is live** on three machines, each with one "host" row
  in uptime-wisp:
  - **bh2:** `host-bh2-4637.mera.network` (TLS)
  - **bh3:** `host-bh3-4637.mera.network` (TLS)
  - **rbserver1:** LAN `:8082`

  Tokens are in `/opt/health-wisp/.health-token` on each host. Deploy
  with `deploy/health/deploy.sh`.
- **bh3** (cinder's host now, plus the user's pruned bitcoind) has
  cinder's shared nginx stream skeleton and ufw (22/80/443, logging
  off). ufw logging is off on bh2 too, and its old ufw logs were
  deleted.

---

## Blocked

- Nothing blocking.
- **Deferred:** the uptime-wisp heartbeat (the user checks the status
  page personally). `heartbeat.url` is ready if that changes.

---

## Next

Nothing scheduled. Deferred by the user (2026-10-02):
- **Image-shipping deploys in the other products** (cinder A024,
  EphemNet A007, persona A011, offgrid A013), filed in their repos.
- **Persisting the Issues log** across restarts.
- **D002 leftovers:** confirm the proposed numbers; country/GeoIP.

Watch:
- **offgridapp's last batch was 14 h old** on 2026-10-04 09:00 UTC.
  Probably a quiet night (the hook only sends when it has data). If
  it's still silent after a visit, ask offgrid's session.
- **bh3's disk is 62%** (cinder plus bitcoind). Its host check alerts
  above 85%.
