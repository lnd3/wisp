# Deployment — wisp.mera.network

Copied and adapted from `persona`'s own `deploy/` (the single-Caddy,
static-site shape), which is itself adapted from `cinder`'s (D007/P008:
HTTP-01-not-TLS-ALPN-01 ACME, the shared nginx `stream{}` skeleton) and
`EphemNet`'s (D003: the `<deploy-root>/<environment>` convention). See
those repos' own `deploy/README.md` for the full architecture and the
real incidents that shaped it; this is the how-to for wisp's slice.

Today it serves a static placeholder page (`site/index.html`). There is
no wisp binary yet. The ingest API/dashboard service slots into
`docker-compose.yml`'s commented-out `wisp` service later, and
wisp-caddy reverse-proxies to it. DNS, nginx and TLS don't change when
that happens.

**Who calls this endpoint:** the products' own backends,
server-to-server, sending unique events or (preferably) pre-aggregated
event data. End users' browsers never talk to wisp. See P001's
2026-09-30 Log.

## Shape

```
                    :443 (SNI passthrough, PROXY protocol)       :80 (ACME HTTP-01 + redirect)
Internet ──▶ nginx (host, shared by every product on bh2) ──────────┬──▶ nginx (host, Host-header vhost)
                │                                                   │
                ▼                                                   ▼
            wisp-caddy  127.0.0.1:9480                          wisp-caddy  127.0.0.1:9220
                │
                ▼
            site/ (static, read-only mount)  →  later: wisp service (no host port)
```

## Privacy: what this deployment must never log

By design, only product backends and the operator call wisp, so no
visitor IP should ever arrive here. A stray browser hit still could:
someone opening the URL, or a product wrongly calling wisp
client-side. CLAUDE.md makes "no raw IP persists anywhere" a hard
invariant, so every layer that could log a request is locked down
anyway, as defense in depth:

| Layer | Handling |
|---|---|
| nginx `:443` (stream, shared skeleton) | No `access_log` in the stream block, and stream logging is off by default. Owned by cinder's shared file; keep it that way. |
| nginx `:80` (`wisp-http01.conf`) | `access_log off` and `error_log … crit` in wisp's own vhost. **Not** in persona's or cinder's copies: Debian's stock `http{}` access log would otherwise record every plain-http visitor. |
| wisp-caddy | No `log` directive in any site block, so there is no access log. The global `log` block drops `http.log.error` and `http.handlers.reverse_proxy`, the loggers that embed the request (and so `remote_ip`). |
| Docker `json-file` logs | Only what the above allows through: Caddy's ACME/TLS lifecycle. The future wisp service must not log IPs either. |

Don't copy cinder's `monitor.sh` here: its usage digest works *by*
reading Caddy access logs with client IPs. That's the opposite of
this project's premise.

**Residual, outside this repo's control:** the shared stream
`server{}`'s error log (host-level `/var/log/nginx/error.log`) can
include `client: <ip>` when a backend connection fails, e.g. while
wisp-caddy is down. Tightening that means a collaborative edit to
cinder's `sni-shared-passthrough.conf` (`error_log … crit` inside
`stream{}`) and affects every product on the host. It's not done
here; see P001's Log.

## One-time setup

Same "server has no GitHub access, push from a dev machine" model as
the other repos on `bh2`. Steps 2, 5 and 6 should already be true from
cinder's setup.

1. On the server:
   `sudo mkdir -p /opt/wisp/live && sudo chown $(whoami) /opt/wisp/live`
2. Confirm the SSH user is in the `docker` group.
3. On the server:
   `mkdir -p /opt/wisp/live/deploy` and create `.env` there by hand
   with `WISP_DOMAIN=wisp.mera.network` (see `deploy/.env.example`).
   It lives only on the server and survives every deploy (`rsync
   --exclude`).
4. **DNS: this is not a registrar step.** `mera.network` is
   NS-delegated to EphemNet's own `ephemnetd`, so `wisp.mera.network`
   needs a zone entry in `bh2`'s live EphemNet `zones.json` (direct
   mode, `ipv4: 158.174.211.245`, the same shape as
   `status.mera.network`/`api.mera.network`), added through EphemNet's
   own operator process, not from this repo. It didn't resolve as of
   2026-09-30. Coordinate it from EphemNet's side. Also make sure no
   EphemNet free-tier customer can register `wisp` under
   `mera.network` first.
5. Confirm the firewall allows `80`/`443`.
6. Confirm nginx, `libnginx-mod-stream`, the `stream-enabled` include
   line and cinder's shared skeleton are in place.
   `configure-nginx.sh` checks for the skeleton and refuses to run
   without it.
7. From the **dev machine**:
   `deploy/configure-nginx.sh bh2 /opt/wisp live`. This installs
   `wisp-live.map` and `wisp-live-http01.conf`. Until then,
   `wisp.mera.network` on `:443` falls through to the shared
   skeleton's `default`, which is EphemNet's relay-proxy.
8. From the **dev machine**: `deploy/deploy.sh bh2 /opt/wisp live`.

**Ports/subnet were checked live on `bh2` (2026-09-30)**: `9480`/`9220`
and `172.27.1.0/24`. See `docker-compose.yml`'s header for what was
already taken, and for why this deliberately doesn't continue
persona's `172.32.x` (that's public address space, outside RFC 1918).

## Redeploying (`deploy/deploy.sh`, from the dev machine)

```bash
deploy/deploy.sh bh2 /opt/wisp live
deploy/deploy.sh bh2 /opt/wisp live --branch=some-other-branch
```

It packages the committed branch with `git archive`, bakes the
build-info footer into the staged `site/index.html` (never the
committed file), `rsync --delete`s it to the server (excluding
`deploy/.env`), then `docker compose up -d`. It refuses to run if
there are uncommitted changes outside `plan/`, if you're on the wrong
branch, or if the server's `deploy/.env` is missing.

## Day-to-day operations (`deploy/ops.sh`)

```bash
deploy/ops.sh bh2 /opt/wisp live status
deploy/ops.sh bh2 /opt/wisp live logs
deploy/ops.sh bh2 /opt/wisp live restart
deploy/ops.sh bh2 /opt/wisp live stop
deploy/ops.sh bh2 /opt/wisp live down
```

## Verifying it works

- `docker exec wisp-live-wisp-caddy-1 find /data/caddy/certificates -type f`
  shows a real cert landed.
- `curl -v https://wisp.mera.network` returns a real cert, the page,
  and the footer showing the commit you just deployed.
- `curl -sI http://wisp.mera.network` returns a `301` to https.
- **Privacy check:** after a few requests, confirm
  `sudo grep -c '<your-own-ip>' /var/log/nginx/access.log` doesn't grow
  from wisp traffic. Also confirm
  `docker logs wisp-live-wisp-caddy-1 2>&1 | grep -c remote_ip` is `0`.

## Second environment (`dev`)

Its own `/opt/wisp/dev` path and `.env`, with a distinct `WISP_DOMAIN`
plus the copy-paste port/subnet block from `deploy/.env.example`:

```bash
deploy/configure-nginx.sh bh2 /opt/wisp dev
deploy/deploy.sh bh2 /opt/wisp dev
```
