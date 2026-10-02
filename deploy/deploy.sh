#!/usr/bin/env bash
# Run this on the DEV machine, not the server — the server has no
# GitHub access, so it can't `git pull` itself. Instead: package the
# current committed HEAD (git archive — only tracked, committed files,
# nothing stray from this machine's own working tree), rsync that
# snapshot over SSH into the server's pre-deployment build folder, then
# trigger the restart there. No container registry: the wisp image is
# built HERE and shipped with docker save | ssh | docker load, so the
# server never compiles anything. wisp-caddy's `caddy:2` is pulled
# directly.
#
# Copied and adapted from persona's own (itself from cinder's/EphemNet's)
# deploy/deploy.sh (same server, `bh2`, same convention).
#
# NOT part of this script, deliberately: nginx install/config, ufw/
# ufw-docker setup, deploy/.env's contents on the server — those are
# one-time, manual, provisioning steps, not something safe to silently
# re-run or overwrite on every deploy.
set -euo pipefail

cd "$(dirname "$0")/.."  # repo root
# shellcheck source=deploy/environments.sh
source deploy/environments.sh

usage() {
	cat >&2 <<USAGE
Usage: $0 <ssh-target> <deploy-root> <environment> [--branch=NAME]

  <ssh-target> <deploy-root>   where to deploy — the actual remote path
                                used is <deploy-root>/<environment>.
                                WISP_DOMAIN is read from that path's
                                own deploy/.env, nowhere else.
  <environment>                 must be one of: $VALID_ENVIRONMENTS — see
                                deploy/environments.sh.
  --branch=NAME                 git ref to deploy (default: main)

Examples:
  $0 deploy@203.0.113.9 /opt/wisp live
  $0 deploy@203.0.113.9 /opt/wisp dev --branch=some-other-branch
USAGE
	exit 1
}

BRANCH="main"
POSITIONAL=()
for arg in "$@"; do
	case "$arg" in
	--branch=*) BRANCH="${arg#--branch=}" ;;
	--*)
		echo "ERROR: unknown flag '$arg'" >&2
		usage
		;;
	*) POSITIONAL+=("$arg") ;;
	esac
done
set -- "${POSITIONAL[@]}"

[ $# -ge 3 ] || usage
DEPLOY_SSH_TARGET="$1"
DEPLOY_ROOT="$2"
ENVIRONMENT="$3"

validate_environment "$ENVIRONMENT"
DEPLOY_REMOTE_PATH="$DEPLOY_ROOT/$ENVIRONMENT"
# Product-qualified, not bare <environment> — cinder's and EphemNet's
# own stacks already use their own product-qualified project names on
# this same server; a bare "live"/"dev" here would land in whichever of
# theirs happened to also be bare, the exact collision both of those
# repos already hit and fixed. See deploy/environments.sh's own header.
COMPOSE_PROJECT="wisp-$ENVIRONMENT"

# Scoped to everything except plan/ — that's documentation, never built
# into an image or shipped to a running container, so a dirty plan/
# (e.g. a just-regenerated plan/INDEX.md timestamp) shouldn't block a
# deploy. Everything else IS packaged by git archive below and ships
# as-is, so an uncommitted edit there really would silently not deploy.
if ! git diff --quiet -- . ':!plan' || ! git diff --cached --quiet -- . ':!plan'; then
	echo "ERROR: uncommitted changes outside plan/ in this working tree — commit or stash them first. Only committed content on $BRANCH ships (git archive), so an uncommitted edit would silently not deploy." >&2
	exit 1
fi

CURRENT_BRANCH="$(git rev-parse --abbrev-ref HEAD)"
if [ "$CURRENT_BRANCH" != "$BRANCH" ]; then
	echo "ERROR: on branch '$CURRENT_BRANCH', not '$BRANCH'. Switch branches or pass --branch=$CURRENT_BRANCH explicitly." >&2
	exit 1
fi

STAGING="$(mktemp -d)"
trap 'rm -rf "$STAGING"' EXIT

GIT_REV="$(git rev-parse --short HEAD)"
BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

echo "==> Packaging $GIT_REV ($BRANCH)..."
git archive "$BRANCH" | tar -x -C "$STAGING"

# site/index.html is a plain static file — wisp-caddy mounts it
# read-only straight from the synced tree below, no build step of its
# own at all — so the build-number/build-time footer can only be baked
# in here, in the staged copy, never the committed file itself (which
# keeps the literal ${WISP_BUILD_INFO} placeholder forever —
# restricted envsubst, same convention EphemNet's own deploy.sh already
# uses, so a real "$" anywhere else in the page is never at risk).
export WISP_BUILD_INFO="build ${GIT_REV} · ${BUILD_TIME}"
BUILD_INFO_VARS='${WISP_BUILD_INFO}'
envsubst "$BUILD_INFO_VARS" <"$STAGING/site/index.html" >"$STAGING/site/index.html.tmp" && mv "$STAGING/site/index.html.tmp" "$STAGING/site/index.html"

# Build the wisp image HERE, from the packaged snapshot, and ship the
# image itself: the server only loads and runs it, with no compiling,
# toolchain pulls or build cache next to every other product. Building on
# bh2 (~2 GB of temporary space) filled its shared disk on 2026-10-02.
case "$(ssh "$DEPLOY_SSH_TARGET" uname -m)" in
x86_64 | amd64) PLATFORM=linux/amd64 ;;
aarch64 | arm64) PLATFORM=linux/arm64 ;;
*)
	echo "ERROR: unsupported server architecture" >&2
	exit 1
	;;
esac
docker info >/dev/null 2>&1 || {
	echo "ERROR: no local Docker daemon — the image is built on this machine, not the server." >&2
	exit 1
}
IMAGE="wisp:$GIT_REV"
echo "==> Building $IMAGE for $PLATFORM (locally)..."
docker build -q --platform "$PLATFORM" -f "$STAGING/deploy/wisp/Dockerfile" -t "$IMAGE" "$STAGING" >/dev/null
echo "==> Shipping $IMAGE ($(docker image ls "$IMAGE" --format '{{.Size}}'))..."
docker save "$IMAGE" | gzip -1 | ssh "$DEPLOY_SSH_TARGET" 'gunzip | docker load -q' >/dev/null

echo "==> Syncing to ${DEPLOY_SSH_TARGET}:${DEPLOY_REMOTE_PATH} ..."
# --delete keeps the remote folder an exact mirror of this commit — but
# deploy/.env (real domain) and deploy/products.json (the product
# registry's token hashes) live only on the server, are never committed,
# and must survive every sync: excluded explicitly so --delete never
# touches them.
rsync -az --delete \
	--exclude 'deploy/.env' \
	--exclude 'deploy/products.json' \
	"$STAGING"/ "${DEPLOY_SSH_TARGET}:${DEPLOY_REMOTE_PATH}/"

echo "==> Building and starting on the server..."
# shellcheck disable=SC2029  # DEPLOY_REMOTE_PATH is ours to expand locally, not the remote's
ssh "$DEPLOY_SSH_TARGET" bash -s <<EOF
set -euo pipefail
cd "$DEPLOY_REMOTE_PATH"
if [ ! -f deploy/.env ]; then
	echo "ERROR: deploy/.env is missing on the server — see deploy/README.md's one-time setup (WISP_DOMAIN is required)." >&2
	exit 1
fi
if [ ! -f deploy/products.json ]; then
	echo "ERROR: deploy/products.json is missing on the server — see deploy/README.md's product registry setup." >&2
	exit 1
fi
# Refuse to start on a nearly full disk: bh2 is shared by every product.
avail_mb=\$(df --output=avail -BM . | tail -1 | tr -dc 0-9)
if [ "\$avail_mb" -lt 300 ]; then
	echo "ERROR: only \${avail_mb} MB free on the server — not building. Free space first (bh2 is shared by every product)." >&2
	exit 1
fi
# Prune dangling images however this ends. Never touches running
# containers, volumes or tagged images. (Nothing is built here, so there
# is no build cache to leave behind.)
trap 'docker image prune -f >/dev/null 2>&1' EXIT
docker image inspect "$IMAGE" >/dev/null # loaded, or stop here
docker tag "$IMAGE" wisp:current
docker compose -p $COMPOSE_PROJECT -f deploy/docker-compose.yml up -d
# Keep the two most recent previous deploys for rollback
# (docker tag wisp:<commit> wisp:current && up -d); remove older ones.
docker image ls wisp --format '{{.Tag}}' | grep -vx -e current -e "$GIT_REV" | tail -n +3 | sed 's/^/wisp:/' | xargs -r docker image rm >/dev/null 2>&1 || true
docker compose -p $COMPOSE_PROJECT -f deploy/docker-compose.yml ps
EOF

echo "==> Done — verify with deploy/README.md's own checklist."
