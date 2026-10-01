#!/usr/bin/env bash
# Deploy (or update) wisp's uptime-kuma instance on any host over SSH:
# sync the compose file and apply it remotely. No git and no build on
# the host: it only pulls the louislam/uptime-kuma:2-slim image.
#
# Host prerequisites: Docker with the Compose v2 plugin
# (`docker compose`), rsync, and the SSH user in the docker group.
#
# Re-running is safe and is also how you update: it pulls the latest
# 2.x image, recreates the container, and prunes the superseded
# (dangling) image. The host's data/ directory (all monitors, channels
# and history) is never synced, overwritten or deleted.
set -euo pipefail

usage() {
	cat >&2 <<USAGE
Usage: $0 <ssh-target> [remote-dir] [--port=N] [--bind=ADDR]

  <ssh-target>   e.g. rbserver1, or user@host (must work as a bare 'ssh <target>')
  [remote-dir]   where the stack lives on the host (default: wisp-uptime-kuma,
                 relative to the SSH user's home)
  --port=N       host port for the web UI (default 3001, uptime-kuma's own);
                 refused if something else on the host already uses it
  --bind=ADDR    address to publish on (default 0.0.0.0 for a LAN box;
                 use 127.0.0.1 behind a TLS reverse proxy on a public server)

Port and bind are remembered on the host (in <remote-dir>/.env), so later
runs without the flags keep them.

Examples:
  $0 rbserver1
  $0 linus4637@rbserver1 wisp-uptime-kuma --port=3002
  $0 bh2 /opt/wisp-uptime-kuma --bind=127.0.0.1 --port=3011
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
REMOTE_DIR="${POSITIONAL[1]:-wisp-uptime-kuma}"

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

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT="wisp-uptime-kuma"
COMPOSE="docker compose -p $PROJECT"

echo "==> Preparing $TARGET:$REMOTE_DIR ..."
ssh "$TARGET" "mkdir -p '$REMOTE_DIR/data' && touch '$REMOTE_DIR/.env'"

# Settings live in the host's .env so a later plain re-run keeps them.
CUR="$(ssh "$TARGET" "cat '$REMOTE_DIR/.env'")"
CUR_PORT="$(printf '%s\n' "$CUR" | sed -n 's/^KUMA_PORT=//p')"
CUR_BIND="$(printf '%s\n' "$CUR" | sed -n 's/^KUMA_BIND=//p')"
PORT="${PORT:-${CUR_PORT:-3001}}"
BIND="${BIND:-${CUR_BIND:-0.0.0.0}}"

# Refuse a port someone else holds. Our own container holding it (a
# redeploy) is fine.
OURS="$(ssh "$TARGET" "cd '$REMOTE_DIR' && $COMPOSE port uptime-kuma 3001 2>/dev/null || true")"
if [ "${OURS##*:}" != "$PORT" ] && ssh "$TARGET" "ss -tlnH 2>/dev/null | awk '{print \$4}' | grep -qE '[:.]$PORT\$'"; then
	echo "ERROR: port $PORT is already in use on $TARGET. Pick another with --port=N." >&2
	ssh "$TARGET" "docker ps --format '  {{.Names}}  {{.Ports}}' | grep -E ':$PORT->' || true" >&2
	exit 1
fi

# Informational: other uptime-kuma containers already on the host.
OTHERS="$(ssh "$TARGET" "docker ps --format '{{.Image}} {{.Names}} ({{.Ports}})' | grep '^louislam/uptime-kuma' | cut -d' ' -f2- | grep -v '^$PROJECT-' || true")"
if [ -n "$OTHERS" ]; then
	echo "    note: another uptime-kuma is already running here: $OTHERS"
	echo "    (this deploy runs alongside it as project '$PROJECT' on port $PORT)"
fi

echo "==> Syncing compose file and settings (port $PORT, bind $BIND)..."
rsync -az "$SCRIPT_DIR/docker-compose.yml" "$TARGET:$REMOTE_DIR/docker-compose.yml"
printf 'KUMA_PORT=%s\nKUMA_BIND=%s\n' "$PORT" "$BIND" | ssh "$TARGET" "cat > '$REMOTE_DIR/.env'"

echo "==> Pulling the image and (re)starting..."
# image prune -f removes only untagged images, e.g. the previous 2.x
# after an update pulled a newer one. Never a tagged or in-use image.
ssh "$TARGET" "cd '$REMOTE_DIR' && $COMPOSE pull -q && $COMPOSE up -d && docker image prune -f >/dev/null && $COMPOSE ps"

HOSTPART="${TARGET#*@}"
[ "$BIND" = "0.0.0.0" ] || HOSTPART="$BIND"
echo
echo "==> Done. Web UI: http://$HOSTPART:$PORT"
echo "    First visit creates the admin account; set up monitors and alerts there"
echo "    (see deploy/uptime-kuma/README.md for the checklist)."
