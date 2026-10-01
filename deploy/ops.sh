#!/usr/bin/env bash
# Day-to-day operations against an already-deployed stack — status,
# logs, start/stop/restart, down. Not deploy.sh's job (that packages
# and ships a new commit, then starts it) — this only ever touches
# already-running containers, never packages or ships code.
#
# Copied and adapted from persona's own (itself from cinder's/EphemNet's)
# deploy/ops.sh (same server, `bh2`, same convention).
set -euo pipefail

cd "$(dirname "$0")/.."  # repo root
# shellcheck source=deploy/environments.sh
source deploy/environments.sh

usage() {
	cat >&2 <<EOF
Usage: $0 <ssh-target> <deploy-root> <environment> <command> [service | product-key]

  <ssh-target> <deploy-root> <environment>   same meaning as deploy.sh's
                                own arguments — the actual remote path
                                used is <deploy-root>/<environment>.
                                <environment> must be one of:
                                $VALID_ENVIRONMENTS

Commands:
  status              docker compose ps
  logs [service]      follow logs (all, or one of: wisp, wisp-caddy)
  start               docker compose up -d — starts already-built images; use deploy.sh to ship new code
  stop                docker compose stop — containers kept, not removed
  restart [service]   restart everything, or one service
  down                stop and remove containers — volumes persist (certs and wisp-data survive this)
  registry            list registered product keys (and short hash prefixes)
  register <product>  add a product token to deploy/products.json — the token is read
                      from stdin and hashed HERE; only the SHA-256 goes to the server.
                      Merges, never overwrites: keeps every other product, adds a
                      second hash for an existing key (rotation, max 2), refuses a
                      hash another product already uses. Backs the old file up,
                      validates, swaps it in atomically, restarts wisp.

Examples:
  $0 deploy@203.0.113.9 /opt/wisp live status
  $0 deploy@203.0.113.9 /opt/wisp live logs
  $0 deploy@203.0.113.9 /opt/wisp live restart wisp
  $0 deploy@203.0.113.9 /opt/wisp dev restart
  echo "\$TOKEN" | $0 deploy@203.0.113.9 /opt/wisp live register cinderapps
EOF
	exit 1
}

[ $# -ge 4 ] || usage
DEPLOY_SSH_TARGET="$1"
DEPLOY_ROOT="$2"
ENVIRONMENT="$3"
COMMAND="$4"
SERVICE="${5:-}"
# A closed set: SERVICE is interpolated into a remote shell command.
# (For `register`, the 5th argument is a product key instead, validated
# there.)
if [ "$COMMAND" != register ]; then
	case "$SERVICE" in
	"" | wisp | wisp-caddy) ;;
	*)
		echo "ERROR: unknown service '$SERVICE' — must be wisp or wisp-caddy" >&2
		exit 1
		;;
	esac
fi

validate_environment "$ENVIRONMENT"
DEPLOY_REMOTE_PATH="$DEPLOY_ROOT/$ENVIRONMENT"

# Product-qualified, not bare <environment> — see deploy/deploy.sh's own
# comment for the real cross-repo Compose-project collision this
# avoids on `bh2`.
COMPOSE_PROJECT="wisp-$ENVIRONMENT"
COMPOSE="docker compose -p $COMPOSE_PROJECT -f deploy/docker-compose.yml"

# shellcheck disable=SC2029  # DEPLOY_REMOTE_PATH is ours to expand locally, not the remote's
run_remote() {
	ssh "$DEPLOY_SSH_TARGET" "cd '$DEPLOY_REMOTE_PATH' && $1"
}

case "$COMMAND" in
status)
	run_remote "$COMPOSE ps"
	;;
logs)
	ssh -t "$DEPLOY_SSH_TARGET" "cd '$DEPLOY_REMOTE_PATH' && $COMPOSE logs -f --tail 200 $SERVICE"
	;;
start | up)
	run_remote "$COMPOSE up -d"
	;;
stop)
	run_remote "$COMPOSE stop"
	;;
restart)
	run_remote "$COMPOSE restart $SERVICE"
	;;
down)
	run_remote "$COMPOSE down"
	;;
registry)
	ssh "$DEPLOY_SSH_TARGET" "python3 -c 'import json,sys; [print(p[\"key\"], *[h[:12]+\"…\" for h in p[\"token_sha256\"]]) for p in json.load(open(sys.argv[1]))[\"products\"]]' '$DEPLOY_REMOTE_PATH/deploy/products.json'"
	;;
register)
	# deploy/products.json is shared, server-only state: every product's
	# session registers itself there independently. Found the hard way
	# (2026-10-01): a hand-written replacement wiped another product's
	# entry. So this only ever merges. See deploy/README.md.
	PRODUCT="$SERVICE"
	if ! [[ "$PRODUCT" =~ ^[a-z0-9][a-z0-9-]{0,62}$ ]]; then
		echo "ERROR: product key must match [a-z0-9][a-z0-9-]{0,62}" >&2
		exit 1
	fi
	if [ -t 0 ]; then
		echo "ERROR: pipe the product's token on stdin, e.g. echo \"\$TOKEN\" | $0 ... register $PRODUCT" >&2
		exit 1
	fi
	IFS= read -r TOKEN || true
	TOKEN="$(printf '%s' "$TOKEN" | tr -d '[:space:]')"
	if [ "${#TOKEN}" -lt 32 ]; then
		echo "ERROR: token too short — generate one with: openssl rand -hex 32" >&2
		exit 1
	fi
	# Same hash as `wisp hash-token`: SHA-256 of the trimmed token.
	HASH="$(printf '%s' "$TOKEN" | sha256sum | cut -d' ' -f1)"
	unset TOKEN
	# PRODUCT and HASH are validated above (key pattern, 64 hex), so they
	# are safe to pass as arguments.
	status=0
	ssh "$DEPLOY_SSH_TARGET" "python3 - '$DEPLOY_REMOTE_PATH/deploy/products.json' '$DEPLOY_ROOT/.products-backups/$ENVIRONMENT' '$PRODUCT' '$HASH'" <<'PY' || status=$?
import json, os, sys, time
path, backups, key, h = sys.argv[1:5]
with open(path) as f:
    reg = json.load(f)
products = reg.setdefault("products", [])
for p in products:
    if p["key"] != key and h in p.get("token_sha256", []):
        sys.exit(f"ERROR: that token is already registered for {p['key']!r}")
entry = next((p for p in products if p["key"] == key), None)
if entry and h in entry["token_sha256"]:
    print(f"{key}: already registered with this token, nothing to do")
    sys.exit(10)  # unchanged: the caller skips the restart
if entry and len(entry["token_sha256"]) >= 2:
    sys.exit(f"ERROR: {key!r} already has 2 token hashes (rotation in progress) — remove the old one by hand first")
if entry:
    entry["token_sha256"].append(h)
    action = "added a second token (rotation)"
else:
    products.append({"key": key, "token_sha256": [h]})
    action = "registered"
os.makedirs(backups, mode=0o700, exist_ok=True)
stamp = time.strftime("products-%Y%m%dT%H%M%SZ", time.gmtime())
backup, n = os.path.join(backups, stamp + ".json"), 1
while os.path.exists(backup):  # never overwrite an earlier backup
    n += 1
    backup = os.path.join(backups, f"{stamp}-{n}.json")
with open(path) as src, open(backup, "w") as dst:
    dst.write(src.read())
os.chmod(backup, 0o600)
tmp = path + ".tmp"
with open(tmp, "w") as f:
    json.dump(reg, f, indent=2)
    f.write(chr(10))
os.chmod(tmp, 0o600)
with open(tmp) as f:
    json.load(f)  # validate before swapping in
os.replace(tmp, path)
print(f"{key}: {action}; products now: {', '.join(p['key'] for p in products)}; backup: {backup}")
PY
	case "$status" in
	0) ;;
	10) exit 0 ;; # already registered: nothing changed, no restart
	*) exit "$status" ;;
	esac
	# A restart (not SIGHUP): the registry is a single-file bind mount,
	# and the atomic rename above gives it a new inode, which only a
	# container restart picks up.
	run_remote "$COMPOSE restart wisp"
	;;
*)
	usage
	;;
esac
