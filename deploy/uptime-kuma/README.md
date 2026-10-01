# uptime-kuma: external uptime monitoring

wisp's answer to P001's "portfolio-wide health monitoring" item. The
tool is [uptime-kuma](https://github.com/louislam/uptime-kuma), and
wisp owns it (the user's decisions, 2026-10-01). The core requirement:
an **external** node actively probes each product's **public** URL and
does its own DNS resolution. Products don't report their own health,
because a dead machine or broken DNS can't send "I'm down".

The deploy is generic: one script, any host you can SSH to. The
intended home is `rbserver1`, which is outside `bh2`, where cinder,
EphemNet, persona and wisp run.

## Prerequisites on the host

- Docker with the Compose v2 plugin (`docker compose version` works).
  The official install script includes it.
- `rsync`.
- Your SSH user in the `docker` group. The script runs `docker`
  without sudo: `sudo usermod -aG docker "$USER"`, then log in again.

## Deploy / update

From a wisp checkout:

```bash
deploy/uptime-kuma/deploy.sh rbserver1                 # → ~/wisp-uptime-kuma on the host, port 3001
deploy/uptime-kuma/deploy.sh rbserver1 --port=3005     # another port (remembered for later runs)
deploy/uptime-kuma/deploy.sh user@host /srv/kuma --bind=127.0.0.1   # behind a TLS proxy
```

- Re-running **is** the update. It pulls the latest
  `louislam/uptime-kuma:1` and recreates the container.
- The host's `data/` directory holds all monitors, notification
  channels and history. The script never syncs, overwrites or deletes
  it.
- It refuses a port that something else on the host already uses. It
  can run alongside another uptime-kuma, because it uses its own
  Compose project, `wisp-uptime-kuma`.
- `--bind` defaults to `0.0.0.0`, which is fine on a LAN-only box like
  `rbserver1`. On anything internet-facing, use `--bind=127.0.0.1` and
  a TLS proxy in front.

**Backup:** stop the stack and copy `<remote-dir>/data/`. The
container takes ownership of `data/` as root, so `tar` needs sudo:
`ssh rbserver1 'cd wisp-uptime-kuma && docker compose -p wisp-uptime-kuma stop && sudo tar czf ~/kuma-backup.tgz data && docker compose -p wisp-uptime-kuma start'`.

## First-run checklist (in the web UI)

1. **Admin account.** The first visit to `http://rbserver1:3001`
   creates it.
2. **Notification channel** (Settings → Notifications). Pick one
   **hosted by a third party**, never on `bh2` or `rbserver1`:
   - ntfy (ntfy.sh, a long random topic, the phone app)
   - Telegram
   - Pushover

   A channel on either machine would go down with the thing it should
   report. Tick "Default enabled" and "Apply on all existing
   monitors".
3. **Monitors.** Always use the **public** URL, never localhost or a
   LAN address, so DNS and TLS are part of what's checked:

   | Product | Monitor | Type, and what counts as "up" |
   |---|---|---|
   | wisp | `https://wisp.mera.network/` | HTTP(s), 200 |
   | wisp ingest | `https://wisp.mera.network/v1/ingest` | HTTP(s); accepted status **405** (GET on a POST endpoint means the API answers) |
   | cinder landing | `https://cinderapps.org/` | HTTP(s), 200 |
   | cinder products | `https://noteshare.cinderapps.org/`, `chatgroups.`, `fileshare.`, `tunnelapp.`, `vigil.` | HTTP(s), one monitor each |
   | EphemNet | `https://eph.network/`, `https://agent.eph.network/` | HTTP(s), 200 |
   | EphemNet DNS | `wisp.mera.network` via resolver `ns1.mera.network` | **DNS** type, A record = `158.174.211.245`. Checks EphemNet's own nameserver answers |
   | persona | `https://solemn.network/` | HTTP(s), 200 |
   | offgrid | `https://offgridapp.mera.network/` | HTTP(s), 200. See the blind spot below |

   Turn on **"Certificate Expiry Notification"** for the HTTPS
   monitors: an ACME renewal failing silently is a classic outage.
   Intervals: 60s with 2–3 retries is a sane default.

## The one blind spot on rbserver1

offgrid also runs on `rbserver1`. If the Pi loses power or its
uplink, offgrid and this monitor go down together, and nothing
reports it. Closing that gap needs a second vantage point **not** on
`rbserver1`, watching for silence. That's a dead-man's switch:
uptime-kuma's **Push** monitor type on a second instance, which
`rbserver1` pings on an interval. P001 records this as a candidate,
not yet set up. This same script deploys that second instance
anywhere.
