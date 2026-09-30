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
Usage: $0 <ssh-target> <deploy-root> <environment> <command>

  <ssh-target> <deploy-root> <environment>   same meaning as deploy.sh's
                                own arguments — the actual remote path
                                used is <deploy-root>/<environment>.
                                <environment> must be one of:
                                $VALID_ENVIRONMENTS

Commands:
  status     docker compose ps
  logs       follow logs
  start      docker compose up -d — starts an already-pulled image; use deploy.sh to sync new site content
  stop       docker compose stop — container kept, not removed
  restart    restart wisp-caddy
  down       stop and remove the container — volumes persist (cert data survives this)

Examples:
  $0 deploy@203.0.113.9 /opt/wisp live status
  $0 deploy@203.0.113.9 /opt/wisp live logs
  $0 deploy@203.0.113.9 /opt/wisp dev restart
EOF
	exit 1
}

[ $# -ge 4 ] || usage
DEPLOY_SSH_TARGET="$1"
DEPLOY_ROOT="$2"
ENVIRONMENT="$3"
COMMAND="$4"

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
	ssh -t "$DEPLOY_SSH_TARGET" "cd '$DEPLOY_REMOTE_PATH' && $COMPOSE logs -f --tail 200"
	;;
start | up)
	run_remote "$COMPOSE up -d"
	;;
stop)
	run_remote "$COMPOSE stop"
	;;
restart)
	run_remote "$COMPOSE restart"
	;;
down)
	run_remote "$COMPOSE down"
	;;
*)
	usage
	;;
esac
