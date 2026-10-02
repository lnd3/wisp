# health-wisp: per-machine disk, memory and load

A tiny reporter, one container per machine, that uptime-wisp polls with
its `host` check. The image is one static Go binary `FROM scratch`
(~5 MB, `cmd/health-wisp`, package `health`, standard library only).

## What it reports

```json
{"host":"bh2","disk_pct":{"root":61},"mem_pct":44,"load_per_cpu":0.2,"sampled":"2026-10-02T16:30:00Z"}
```

- **Disk:** used %, rounded up like df's `Use%`, current value. A disk
  can fill in minutes, so this is never smoothed.
- **Memory:** (MemTotal − MemAvailable) / MemTotal, whole %.
- **Load:** the kernel's 15-minute load average ÷ the host's CPUs, to
  0.05. Slow on purpose: nobody can watch their own requests move it.

Totals only: no process, container or version information. A
background loop samples every 30s and requests get that cached sample.
health-wisp never decides what "healthy" means. The thresholds live in
uptime-wisp's config, where Reload can change them.

**Access:**
- **Token:** `GET /` needs `Authorization: Bearer <token>`. The host
  keeps only the token's SHA-256 (`.env`).
- **Rate limit:** one global limit (2/s, bursts of 20), applied before
  the token check, so guessing tokens is slow too.
- **No addresses:** no client address is looked at or logged, and Go's
  own HTTP error log is discarded because it names clients.
- **`/healthz`:** open. It says only "ok", for Docker.
- **Stale samples:** both endpoints answer 503 if no sample has been
  taken for three intervals.

Inside the container, `/proc/loadavg`, `/proc/meminfo` and `/proc/stat`
already show host-wide values. Disk usage comes from statfs, which
reports the whole filesystem for any path on it. So the only mount is
one empty directory (`fs-root/` beside the compose file) on the
host's root disk. Neither the host's `/` nor its `/proc` is mounted.

## Deploy / update

```bash
# internet-facing, behind cinder's shared nginx (TLS by a Caddy sidecar):
deploy/health/deploy.sh bh2 /opt/health-wisp --host=bh2 --domain=host-bh2-4637.mera.network
# LAN box (plain port; default 8081):
deploy/health/deploy.sh rbserver1 /opt/health-wisp --host=rbserver1 --port=8082   # 8081 is taken there
```

- **Build:** the image is built on the dev machine and shipped
  (`docker save | ssh | docker load`). Nothing is built on the host,
  and the two previous images are kept for rollback.
- **Settings:** remembered in `<remote-dir>/.env`. Re-running with no
  flags is the update path.
- **Token:** the first deploy generates it on the host into
  `<remote-dir>/.health-token` (0600), and it's never printed. To
  rotate it, delete the `HEALTH_TOKEN_SHA256` line from `.env`, re-run
  the deploy, and update uptime-wisp's check.
- **TLS mode** (`--domain`):
  - **DNS:** the name must already resolve to the host. For
    `*.mera.network` that's an EphemNet zones entry.
  - **nginx:** the script installs health-wisp's own nginx fragments
    (`stream-backends.d/health-wisp.map`,
    `sites-enabled/health-wisp-http01.conf`) and never touches the
    shared skeleton.
  - **bh2 ports and subnet:** `9230`/`9490`, `172.27.21.0/24`.
  - **Caddy image:** `caddy:2` is already there for wisp-caddy, so it
    costs no extra disk.
  - **Logging:** the same lockdown as wisp-caddy: no access logs, and
    nginx's error log at `crit`.

## uptime-wisp check

```json
{"name": "bh2 host", "type": "host",
 "url": "https://host-bh2-4637.mera.network/",
 "token": "<contents of bh2:/opt/health-wisp/.health-token>",
 "max_disk_pct": 85, "max_mem_pct": 90, "max_load_per_cpu": 2}
```

The limits are optional and default to 90 / 95 / 2.0. The status page shows one
row per machine, e.g. `disk root 61% · mem 44% · load 0.20/cpu`, and
a breach is listed first. Alerts use the usual `failures_before_alert`
and ntfy channel.
