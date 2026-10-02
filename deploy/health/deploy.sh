#!/usr/bin/env bash
# Deploy (or update) health-wisp on any host over SSH.
#
# Builds the image here (static binary, FROM scratch, ~5 MB) for the
# host's architecture and ships it (docker save | ssh | docker load):
# nothing is built on the host. Re-running is safe and is the update
# path; settings given once are remembered in <remote-dir>/.env.
#
# Two modes:
#   --domain=FQDN   internet-facing host behind cinder's shared nginx
#                   (bh2): a Caddy sidecar terminates TLS for FQDN, and
#                   this script installs health-wisp's own nginx
#                   fragments. FQDN must already resolve to the host.
#   (no domain)     LAN box: a plain host port (--port, --bind).
#
# The first deploy generates the access token on the host:
# <remote-dir>/.health-token (0600) holds it, .env only its SHA-256. Put
# the token in uptime-wisp's "host" check.
set -euo pipefail

usage() {
	cat >&2 <<USAGE
Usage: $0 <ssh-target> <remote-dir> [--host=NAME] [--domain=FQDN] [--port=N] [--bind=ADDR]

  <ssh-target>   e.g. bh2 or rbserver1 (must work as a bare 'ssh <target>')
  <remote-dir>   e.g. /opt/health-wisp (writable by the SSH user)
  --host=NAME    machine name in the report (default: the host's hostname)
  --domain=FQDN  TLS mode behind the shared nginx (see above)
  --port=N       LAN mode: host port (default 8081)
  --bind=ADDR    LAN mode: address to publish on (default 0.0.0.0)

Examples:
  $0 bh2 /opt/health-wisp --host=bh2 --domain=bh2.health.wisp.mera.network
  $0 rbserver1 /opt/health-wisp --host=rbserver1
USAGE
	exit 1
}

HOST="" DOMAIN="" PORT="" BIND="" POSITIONAL=()
for arg in "$@"; do
	case "$arg" in
	--host=*) HOST="${arg#--host=}" ;;
	--domain=*) DOMAIN="${arg#--domain=}" ;;
	--port=*) PORT="${arg#--port=}" ;;
	--bind=*) BIND="${arg#--bind=}" ;;
	-h | --help) usage ;;
	--*)
		echo "ERROR: unknown flag '$arg'" >&2
		usage
		;;
	*) POSITIONAL+=("$arg") ;;
	esac
done
[ "${#POSITIONAL[@]}" -eq 2 ] || usage
TARGET="${POSITIONAL[0]}"
REMOTE_DIR="${POSITIONAL[1]}"

# Validated: these end up inside remote shell commands and .env.
[[ "$REMOTE_DIR" =~ ^[A-Za-z0-9._/~-]+$ ]] || { echo "ERROR: remote-dir may contain only letters, digits and . _ / ~ -" >&2; exit 1; }
[ -z "$HOST" ] || [[ "$HOST" =~ ^[A-Za-z0-9._-]+$ ]] || { echo "ERROR: --host may contain only letters, digits and . _ -" >&2; exit 1; }
[ -z "$DOMAIN" ] || [[ "$DOMAIN" =~ ^[a-z0-9.-]+\.[a-z]+$ ]] || { echo "ERROR: --domain must be a lowercase hostname" >&2; exit 1; }
[ -z "$PORT" ] || { [[ "$PORT" =~ ^[0-9]+$ ]] && [ "$PORT" -ge 1 ] && [ "$PORT" -le 65535 ]; } || { echo "ERROR: --port must be 1-65535" >&2; exit 1; }
[ -z "$BIND" ] || [[ "$BIND" =~ ^[0-9a-fA-F.:]+$ ]] || { echo "ERROR: --bind must be an IP address" >&2; exit 1; }

cd "$(dirname "$0")/../.." # repo root
PROJECT="health-wisp"

echo "==> Checking $TARGET..."
ssh "$TARGET" "mkdir -p '$REMOTE_DIR' && test -w '$REMOTE_DIR'" || {
	echo "ERROR: can't create or write $REMOTE_DIR on $TARGET as the SSH user (for /opt: sudo mkdir -p $REMOTE_DIR && sudo chown \$USER: $REMOTE_DIR)." >&2
	exit 1
}
case "$(ssh "$TARGET" uname -m)" in
x86_64 | amd64) GOARCH=amd64 ;;
aarch64 | arm64) GOARCH=arm64 ;;
*)
	echo "ERROR: unsupported host architecture" >&2
	exit 1
	;;
esac
docker info >/dev/null 2>&1 || { echo "ERROR: no local Docker daemon — the image is built on this machine, not the host." >&2; exit 1; }

# Settings: flags win, then what the host's .env remembers, then defaults.
CUR="$(ssh "$TARGET" "cat '$REMOTE_DIR/.env' 2>/dev/null || true")"
cur() { printf '%s\n' "$CUR" | sed -n "s/^$1=//p" | tail -1; }
HOST="${HOST:-$(cur HEALTH_HOST)}"
HOST="${HOST:-$(ssh "$TARGET" hostname -s)}"
DOMAIN="${DOMAIN:-$(cur HEALTH_DOMAIN)}"
TOKEN_HASH="$(cur HEALTH_TOKEN_SHA256)"
HTTPS_PORT="$(cur HEALTH_CADDY_HTTPS_PORT)" HTTPS_PORT="${HTTPS_PORT:-9490}"
HTTP_PORT="$(cur HEALTH_CADDY_HTTP_PORT)" HTTP_PORT="${HTTP_PORT:-9230}"
PORT="${PORT:-$(cur HEALTH_PORT)}" PORT="${PORT:-8081}"
BIND="${BIND:-$(cur HEALTH_BIND)}" BIND="${BIND:-0.0.0.0}"
if [ -n "$DOMAIN" ]; then
	MODE=tls OVERLAY=docker-compose.tls.yml WANT_PORTS="$HTTPS_PORT $HTTP_PORT"
else
	MODE=lan OVERLAY=docker-compose.lan.yml WANT_PORTS="$PORT"
fi
COMPOSE="docker compose -p $PROJECT"

# Refuse ports someone else holds; our own containers holding them is fine.
OURS="$(ssh "$TARGET" "docker ps --filter label=com.docker.compose.project=$PROJECT --format '{{.Ports}}'" || true)"
for p in $WANT_PORTS; do
	if ! grep -qE "[:.]$p->" <<<"$OURS" && ssh "$TARGET" "ss -tlnH 2>/dev/null | awk '{print \$4}' | grep -qE '[:.]$p\$'"; then
		echo "ERROR: port $p is already in use on $TARGET." >&2
		exit 1
	fi
done

if [ "$MODE" = tls ]; then
	ssh "$TARGET" "test -f /etc/nginx/stream-enabled/sni-shared-passthrough.conf" || {
		echo "ERROR: TLS mode needs cinder's shared nginx stream skeleton on $TARGET (see deploy/README.md)." >&2
		exit 1
	}
	IP="$(dig +short "$DOMAIN" @1.1.1.1 | tail -1)"
	[ -n "$IP" ] || { echo "ERROR: $DOMAIN doesn't resolve yet — add its DNS record first (for *.mera.network: EphemNet's zones)." >&2; exit 1; }
fi

GIT_REV="$(git rev-parse --short HEAD)"
git diff --quiet -- health cmd/health-wisp deploy/health || GIT_REV="$GIT_REV-dirty"
IMAGE="health-wisp:$GIT_REV"
STAGING="$(mktemp -d)"
trap 'rm -rf "$STAGING"' EXIT
echo "==> Building $IMAGE for linux/$GOARCH (locally)..."
CGO_ENABLED=0 GOOS=linux GOARCH=$GOARCH go build -trimpath -ldflags="-s -w" -o "$STAGING/health-wisp" ./cmd/health-wisp
docker build -q --platform "linux/$GOARCH" -f deploy/health/Dockerfile -t "$IMAGE" "$STAGING" >/dev/null

avail_mb="$(ssh "$TARGET" "df --output=avail -BM '$REMOTE_DIR' | tail -1 | tr -dc 0-9")"
if [ "$avail_mb" -lt 100 ]; then
	echo "ERROR: only ${avail_mb} MB free on $TARGET — not shipping. Free space first." >&2
	exit 1
fi
echo "==> Shipping $IMAGE ($(docker image ls "$IMAGE" --format '{{.Size}}'))..."
docker save "$IMAGE" | gzip -1 | ssh "$TARGET" 'gunzip | docker load -q' >/dev/null

echo "==> Syncing ($MODE mode, host $HOST)..."
rsync -az deploy/health/docker-compose.yml deploy/health/$OVERLAY deploy/health/Caddyfile deploy/health/README.md "$TARGET:$REMOTE_DIR/"

if [ -z "$TOKEN_HASH" ]; then
	echo "==> Generating the access token on $TARGET (in $REMOTE_DIR/.health-token)..."
	# Generated on the host and never printed here: copy it into
	# uptime-wisp's config from that file.
	TOKEN_HASH="$(ssh "$TARGET" "cd '$REMOTE_DIR' && umask 077 && head -c 32 /dev/urandom | base64 | tr -d '=+/\n' > .health-token && tr -d '\n' < .health-token | sha256sum | cut -d' ' -f1")"
fi
[[ "$TOKEN_HASH" =~ ^[0-9a-f]{64}$ ]] || { echo "ERROR: HEALTH_TOKEN_SHA256 on $TARGET is not 64 hex chars." >&2; exit 1; }

{
	echo "# Written by deploy/health/deploy.sh; flags on the next run override."
	echo "COMPOSE_FILE=docker-compose.yml:$OVERLAY"
	echo "HEALTH_HOST=$HOST"
	echo "HEALTH_TOKEN_SHA256=$TOKEN_HASH"
	if [ "$MODE" = tls ]; then
		echo "HEALTH_DOMAIN=$DOMAIN"
		echo "HEALTH_CADDY_HTTPS_PORT=$HTTPS_PORT"
		echo "HEALTH_CADDY_HTTP_PORT=$HTTP_PORT"
	else
		echo "HEALTH_PORT=$PORT"
		echo "HEALTH_BIND=$BIND"
	fi
} | ssh "$TARGET" "umask 077 && cat > '$REMOTE_DIR/.env'"

if [ "$MODE" = tls ]; then
	echo "==> Installing health-wisp's nginx fragments..."
	export HEALTH_DOMAIN="$DOMAIN" HEALTH_CADDY_HTTPS_PORT="$HTTPS_PORT" HEALTH_CADDY_HTTP_PORT="$HTTP_PORT"
	VARS='${HEALTH_DOMAIN} ${HEALTH_CADDY_HTTPS_PORT} ${HEALTH_CADDY_HTTP_PORT}'
	envsubst "$VARS" <deploy/health/nginx/health.map.template | ssh "$TARGET" "sudo tee /etc/nginx/stream-backends.d/health-wisp.map >/dev/null"
	envsubst "$VARS" <deploy/health/nginx/health-http01.conf.template | ssh "$TARGET" "sudo tee /etc/nginx/sites-enabled/health-wisp-http01.conf >/dev/null"
	ssh "$TARGET" "sudo nginx -t 2>&1 | tail -1 && sudo systemctl reload nginx"
fi

echo "==> Starting..."
ssh "$TARGET" bash -s <<EOF
set -euo pipefail
cd '$REMOTE_DIR'
mkdir -p fs-root
docker tag '$IMAGE' health-wisp:current
$COMPOSE up -d --remove-orphans --quiet-pull 2>&1 | tail -3
# Keep the two previous images for rollback.
docker image ls health-wisp --format '{{.Tag}}' | grep -vx -e current -e '$GIT_REV' | tail -n +3 | sed 's/^/health-wisp:/' | xargs -r docker image rm >/dev/null 2>&1 || true
docker image prune -f >/dev/null 2>&1 || true
$COMPOSE ps --format '{{.Name}} {{.Status}}'
EOF

if [ "$MODE" = tls ]; then URL="https://$DOMAIN/"; else
	H="$BIND"; [ "$H" = 0.0.0.0 ] && H="$(ssh "$TARGET" "hostname -I | cut -d' ' -f1")"
	URL="http://$H:$PORT/"
fi
echo "==> Checking $URL with the token (TLS mode: the first certificate can take a minute)..."
for i in 1 2 3 4 5 6 7 8 9 10 11 12; do
	# The header goes in on stdin, so the token never shows in ps.
	if OUT="$(ssh "$TARGET" "printf 'Authorization: Bearer %s\n' \"\$(cat '$REMOTE_DIR/.health-token')\" | curl -sf --max-time 5 -H @- '$URL'")"; then
		echo "    $OUT"
		break
	fi
	[ "$i" = 12 ] && { echo "WARNING: no answer yet from $URL — check '$COMPOSE logs' on $TARGET." >&2; break; }
	sleep 5
done
echo
echo "==> Done. uptime-wisp check: {\"name\":\"$HOST host\",\"type\":\"host\",\"url\":\"$URL\",\"token\":\"<$TARGET:$REMOTE_DIR/.health-token>\"}"
