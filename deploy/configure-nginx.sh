#!/usr/bin/env bash
# Run from the dev machine, over SSH — installs/updates THIS repo's own
# nginx site configuration on the server. Copied from persona's own —
# same shape and reasoning as cinder's/EphemNet's
# deploy/configure-nginx.sh (D007's multi-repo nginx convention) —
# adapted here, not reinvented, because this script
# must NOT touch the shared skeleton file
# (/etc/nginx/stream-enabled/sni-shared-passthrough.conf — cinder
# created it first, and nginx allows only one `stream{}`/
# `server{listen 443}` combination for the whole host; ownership isn't
# cinder's specifically, though — see that file's own header). See
# cinder's own deploy/README.md, "Adding another product's nginx
# routing (EphemNet, persona, ...)" — this script is exactly that
# section's own worked example, made real for this repo.
#
# This script only ever writes files that are exclusively wisp's own
# (deploy/nginx/stream-backends.d/wisp.map.template,
# deploy/nginx/sites-enabled/wisp-http01.conf.template — both
# rendered locally via envsubst before shipping, see below, and named
# wisp-<environment> — see ENV_TAG's own comment for why the repo
# name is folded in). It never writes to the shared
# /etc/nginx/nginx.conf, and never writes to
# stream-enabled/sni-shared-passthrough.conf.
set -euo pipefail

cd "$(dirname "$0")/.."  # repo root
# shellcheck source=deploy/environments.sh
source deploy/environments.sh

usage() {
	cat >&2 <<EOF
Usage: $0 <ssh-target> <deploy-root> <environment>

  <ssh-target> <deploy-root> <environment>   same meaning as deploy.sh's
                                own arguments — the actual remote path
                                used is <deploy-root>/<environment>.
                                <environment> must be one of:
                                $VALID_ENVIRONMENTS
                                WISP_DOMAIN and the port overrides
                                are read from that path's own
                                deploy/.env, nowhere else.

Examples:
  $0 deploy@203.0.113.9 /opt/wisp live
  $0 deploy@203.0.113.9 /opt/wisp dev
EOF
	exit 1
}
[ $# -ge 3 ] || usage
DEPLOY_SSH_TARGET="$1"
DEPLOY_ROOT="$2"
ENVIRONMENT="$3"

validate_environment "$ENVIRONMENT"
DEPLOY_REMOTE_PATH="$DEPLOY_ROOT/$ENVIRONMENT"

# ENV_TAG names files in /etc/nginx/stream-backends.d/ and
# sites-enabled/ — directories shared across multiple repos by design
# (cinder, EphemNet, offgridapp and persona all already run their own equivalents of this
# script on the same server, `bh2`). "live"/"dev" alone would be a real
# collision risk there — see cinder's own commit history for the
# incident that established this convention; reused proactively here
# rather than rediscovered.
ENV_TAG="wisp-$ENVIRONMENT"

echo "==> Reading WISP_DOMAIN and port overrides from the server's own deploy/.env..."
# [[:space:]]* tolerates a leading-whitespace .env line — see cinder's
# own configure-nginx.sh for the real incident (a line copied from
# .env.example's indented documentation, whitespace and all, silently
# matched nothing).
if ! DOMAIN_VARS="$(ssh "$DEPLOY_SSH_TARGET" "grep -E '^[[:space:]]*(WISP_DOMAIN|WISP_CADDY_HTTPS_PORT|WISP_CADDY_HTTP_PORT)=' '$DEPLOY_REMOTE_PATH/deploy/.env' || true")"; then
	DOMAIN_VARS=""
fi
WISP_DOMAIN="$(printf '%s\n' "$DOMAIN_VARS" | sed -n 's/^[[:space:]]*WISP_DOMAIN=//p')"
: "${WISP_DOMAIN:?WISP_DOMAIN missing/empty in $DEPLOY_SSH_TARGET:$DEPLOY_REMOTE_PATH/deploy/.env — see deploy/.env.example}"

# Port overrides: read from the same lookup above, falling back to
# docker-compose.yml's own defaults if unset, so nginx and Caddy always
# agree on which port a given environment's sidecar actually publishes.
# Fails loud on a present-but-malformed value (trailing text, an inline
# "#" comment) rather than silently falling back — see cinder's own
# configure-nginx.sh for the real incident that shape prevents.
port_or_default() {
	local name="$1" default="$2"
	local value
	value="$(printf '%s\n' "$DOMAIN_VARS" | sed -n "s/^[[:space:]]*${name}=//p" | sed -E 's/[[:space:]]*(#.*)?$//')"
	if [ -n "$value" ] && ! [[ "$value" =~ ^[0-9]+$ ]]; then
		echo "ERROR: $name in $DEPLOY_SSH_TARGET:$DEPLOY_REMOTE_PATH/deploy/.env is not a clean port number: '$value' — check for a stray inline comment or trailing text on that line." >&2
		exit 1
	fi
	printf '%s' "${value:-$default}"
}
# Must match deploy/docker-compose.yml's own defaults exactly.
WISP_CADDY_HTTPS_PORT="$(port_or_default WISP_CADDY_HTTPS_PORT 9480)"
WISP_CADDY_HTTP_PORT="$(port_or_default WISP_CADDY_HTTP_PORT 9220)"

# export, not just assign: envsubst below runs as a separate process.
export WISP_DOMAIN WISP_CADDY_HTTPS_PORT WISP_CADDY_HTTP_PORT

echo "==> Checking the shared stream skeleton is already installed..."
# This repo never installs it — whichever repo got there first did
# (cinder, in practice). If it's missing, that's a one-time setup step
# owned by whoever runs their own configure-nginx.sh first, not
# something to fix from here.
if ! ssh "$DEPLOY_SSH_TARGET" "test -f /etc/nginx/stream-enabled/sni-shared-passthrough.conf"; then
	cat >&2 <<MSG
ERROR: /etc/nginx/stream-enabled/sni-shared-passthrough.conf doesn't
exist on $DEPLOY_SSH_TARGET yet. That is the shared stream skeleton
file (see cinder's own D007) — run cinder's own
deploy/configure-shared-nginx.sh first (it created this file), then
re-run this script.
MSG
	exit 1
fi

echo "==> Installing this repo's own fragments (env-tag: $ENV_TAG)..."
ssh "$DEPLOY_SSH_TARGET" "sudo mkdir -p /etc/nginx/stream-backends.d /etc/nginx/sites-enabled"
ENVSUBST_VARS='${WISP_DOMAIN} ${WISP_CADDY_HTTPS_PORT} ${WISP_CADDY_HTTP_PORT}'
envsubst "$ENVSUBST_VARS" \
	< deploy/nginx/stream-backends.d/wisp.map.template \
	| ssh "$DEPLOY_SSH_TARGET" "sudo tee /etc/nginx/stream-backends.d/${ENV_TAG}.map > /dev/null"
envsubst "$ENVSUBST_VARS" \
	< deploy/nginx/sites-enabled/wisp-http01.conf.template \
	| ssh "$DEPLOY_SSH_TARGET" "sudo tee /etc/nginx/sites-enabled/${ENV_TAG}-http01.conf > /dev/null"

echo "==> Validating and reloading..."
ssh "$DEPLOY_SSH_TARGET" "sudo nginx -t && sudo systemctl reload nginx"

echo "==> Done."
