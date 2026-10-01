#!/usr/bin/env bash
# Deploy (or update) uptime-wisp on any host over SSH.
#
# Cross-compiles cmd/uptime-wisp for the host's own OS/arch here (static,
# CGO off), ships the binary + Dockerfile + compose file, and builds a
# tiny alpine image there. The host needs Docker (with the Compose v2
# plugin), rsync, and the SSH user in the docker group; no Go toolchain.
#
# Re-running is safe and is the update path. The host's config.json
# (checks + alert secrets) is never synced or overwritten.
set -euo pipefail

usage() {
	cat >&2 <<USAGE
Usage: $0 <ssh-target> [remote-dir] [--port=N] [--bind=ADDR]

  <ssh-target>   e.g. rbserver1, or user@host (must work as a bare 'ssh <target>')
  [remote-dir]   where it lives on the host (default: uptime-wisp, relative
                 to the SSH user's home; must be writable by that user)
  --port=N       host port for the status page (default 8080)
  --bind=ADDR    address to publish on (default 0.0.0.0 for a LAN box;
                 127.0.0.1 on an internet-facing host)

Port and bind are remembered on the host (in <remote-dir>/.env).

First deploy: the host needs <remote-dir>/config.json. The script stops and
tells you how to create it from config.example.json.

Examples:
  $0 rbserver1 /opt/uptime-wisp
  $0 rbserver1 /opt/uptime-wisp --port=8090
USAGE
	exit 1
}

PORT="" BIND="" POSITIONAL=()
for arg in "$@"; do
	case "$arg" in
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
[ "${#POSITIONAL[@]}" -ge 1 ] && [ "${#POSITIONAL[@]}" -le 2 ] || usage
TARGET="${POSITIONAL[0]}"
REMOTE_DIR="${POSITIONAL[1]:-uptime-wisp}"

# Validated: these end up inside remote shell commands.
if ! [[ "$REMOTE_DIR" =~ ^[A-Za-z0-9._/~-]+$ ]]; then
	echo "ERROR: remote-dir may contain only letters, digits and . _ / ~ -" >&2
	exit 1
fi
if [ -n "$PORT" ] && ! { [[ "$PORT" =~ ^[0-9]+$ ]] && [ "$PORT" -ge 1 ] && [ "$PORT" -le 65535 ]; }; then
	echo "ERROR: --port must be 1-65535" >&2
	exit 1
fi
if [ -n "$BIND" ] && ! [[ "$BIND" =~ ^[0-9a-fA-F.:]+$ ]]; then
	echo "ERROR: --bind must be an IP address (e.g. 0.0.0.0 or 127.0.0.1)" >&2
	exit 1
fi

cd "$(dirname "$0")/../.." # repo root
PROJECT="uptime-wisp"
COMPOSE="docker compose -p $PROJECT"

echo "==> Checking $TARGET..."
ssh "$TARGET" "mkdir -p '$REMOTE_DIR' && test -w '$REMOTE_DIR'" || {
	echo "ERROR: can't create or write $REMOTE_DIR on $TARGET as the SSH user (for /opt: sudo mkdir -p $REMOTE_DIR && sudo chown \$USER: $REMOTE_DIR)." >&2
	exit 1
}
if ! ssh "$TARGET" "test -f '$REMOTE_DIR/config.json'"; then
	scp -q deploy/uptime/config.example.json "$TARGET:$REMOTE_DIR/config.example.json"
	cat >&2 <<MSG
ERROR: $TARGET:$REMOTE_DIR/config.json doesn't exist yet. It holds your checks
and the alert channel's secret, so it's created on the host, never shipped:

  ssh $TARGET 'cp $REMOTE_DIR/config.example.json $REMOTE_DIR/config.json && chmod 600 $REMOTE_DIR/config.json'
  ssh $TARGET 'nano $REMOTE_DIR/config.json'     # set your ntfy topic (or webhook), adjust checks

then re-run this script.
MSG
	exit 1
fi

case "$(ssh "$TARGET" uname -m)" in
x86_64 | amd64) GOARCH=amd64 ;;
aarch64 | arm64) GOARCH=arm64 ;;
armv7l) GOARCH=arm GOARM=7 ;;
*)
	echo "ERROR: unsupported host architecture" >&2
	exit 1
	;;
esac

CUR="$(ssh "$TARGET" "cat '$REMOTE_DIR/.env' 2>/dev/null || true")"
CUR_PORT="$(printf '%s\n' "$CUR" | sed -n 's/^UPTIME_PORT=//p')"
CUR_BIND="$(printf '%s\n' "$CUR" | sed -n 's/^UPTIME_BIND=//p')"
PORT="${PORT:-${CUR_PORT:-8080}}"
BIND="${BIND:-${CUR_BIND:-0.0.0.0}}"

# Refuse a port someone else holds; our own container holding it is fine.
OURS="$(ssh "$TARGET" "cd '$REMOTE_DIR' && $COMPOSE port uptime-wisp 8080 2>/dev/null || true")"
if [ "${OURS##*:}" != "$PORT" ] && ssh "$TARGET" "ss -tlnH 2>/dev/null | awk '{print \$4}' | grep -qE '[:.]$PORT\$'"; then
	echo "ERROR: port $PORT is already in use on $TARGET. Pick another with --port=N." >&2
	exit 1
fi

STAGING="$(mktemp -d)"
trap 'rm -rf "$STAGING"' EXIT
echo "==> Building uptime-wisp for linux/$GOARCH..."
CGO_ENABLED=0 GOOS=linux GOARCH=$GOARCH ${GOARM:+GOARM=$GOARM} \
	go build -trimpath -ldflags="-s -w" -o "$STAGING/uptime-wisp" ./cmd/uptime-wisp
cp deploy/uptime/Dockerfile deploy/uptime/docker-compose.yml deploy/uptime/config.example.json "$STAGING/"

echo "==> Validating the host's config.json with this build..."
# Check the host's real config with the new binary, exactly as the
# container will run it (-listen :8080, so e.g. the auth requirement
# applies), before replacing the running prober: a config the new
# version rejects must not take monitoring down.
scp -q "$STAGING/uptime-wisp" "$TARGET:$REMOTE_DIR/.uptime-wisp.check"
if ! ssh "$TARGET" "cd '$REMOTE_DIR' && ./.uptime-wisp.check -config config.json -listen :8080 -check-config; rc=\$?; rm -f .uptime-wisp.check; exit \$rc"; then
	echo "ERROR: config.json on $TARGET is invalid for this version (above). Nothing was changed." >&2
	exit 1
fi

echo "==> Syncing (port $PORT, bind $BIND)..."
rsync -az --exclude config.json "$STAGING/" "$TARGET:$REMOTE_DIR/"
printf 'UPTIME_PORT=%s\nUPTIME_BIND=%s\n' "$PORT" "$BIND" | ssh "$TARGET" "cat > '$REMOTE_DIR/.env'"

echo "==> Building the image and (re)starting..."
ssh "$TARGET" "cd '$REMOTE_DIR' && $COMPOSE up -d --build --quiet-pull 2>&1 | tail -3 && docker image prune -f >/dev/null && $COMPOSE ps"

HOSTPART="${TARGET#*@}"
[ "$BIND" = "0.0.0.0" ] || HOSTPART="$BIND"
echo
echo "==> Done. Status page: http://$HOSTPART:$PORT"
echo "    Prove alerts reach you:  ssh $TARGET 'cd $REMOTE_DIR && $COMPOSE exec uptime-wisp uptime-wisp -config /etc/uptime-wisp/config.json -test-alert'"
