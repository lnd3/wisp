# Sourced by deploy.sh, ops.sh, and configure-nginx.sh — the single
# source of truth for "which deployment is this," validated identically
# by all three. Copied directly from persona's own (itself from cinder's/EphemNet's)
# deploy/environments.sh (same server, `bh2`, same convention) rather
# than reinvented — see cinder's own D007/P008 and EphemNet's D003 for
# the real incidents this fixed there: a free-form remote path, a
# free-form nginx env-tag, and a Compose project name derived from a
# directory basename could all silently drift apart from each other or
# collide across repos on the same shared server. Used bare only as the
# last path segment under <deploy-root> (isolated by that path's own
# structure); every other use folds this repo's own name in first
# (`wisp-live`/`wisp-dev`, for both the nginx fragment filenames
# and the Docker Compose project name) — see configure-nginx.sh's and
# deploy.sh's own comments.
VALID_ENVIRONMENTS="live dev"

# validate_environment ENV — exits with a clear error naming the
# allowed set if ENV isn't one of them. Extend VALID_ENVIRONMENTS only
# when a real new environment actually exists — never pass an
# arbitrary string here to work around this check instead.
validate_environment() {
	local env="$1"
	case " $VALID_ENVIRONMENTS " in
	*" $env "*) ;;
	*)
		echo "ERROR: '$env' is not a valid environment — must be one of: $VALID_ENVIRONMENTS. Extend VALID_ENVIRONMENTS in deploy/environments.sh if a real new environment needs one." >&2
		exit 1
		;;
	esac
}
