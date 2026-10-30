#!/usr/bin/env bash
# Shared paths for the verify-genesis helpers. Sourced, not run.
# shellcheck disable=SC2034 # variables are used by the sourcing scripts
set -euo pipefail

REPO=$(git rev-parse --show-toplevel)
VERIFY_ROOT=${GENESIS_VERIFY_ROOT:-${TMPDIR:-/tmp}/genesis-verify}
VERIFY_ROOT=${VERIFY_ROOT%/}

# run_paths <run-id>: sets RUN_ID, RUN (scratch, removed by cleanup) and
# EVIDENCE (kept after cleanup).
run_paths() {
	RUN_ID=${1:?usage: <run-id>}
	RUN="$VERIFY_ROOT/runs/$RUN_ID"
	EVIDENCE="$VERIFY_ROOT/evidence/$RUN_ID"
}

# Go and Docker keep their caches and contexts under HOME: pin them to the
# real ones before genesis runs with the run's own HOME.
export GOCACHE=${GOCACHE:-$(go env GOCACHE)}
export GOMODCACHE=${GOMODCACHE:-$(go env GOMODCACHE)}
export GOPATH=${GOPATH:-$(go env GOPATH)}
export DOCKER_CONFIG=${DOCKER_CONFIG:-$HOME/.docker}

# mask: hides the age master key in transcripts.
mask() {
	sed -E 's/AGE-SECRET-KEY-1[0-9A-Z]+/AGE-SECRET-KEY-<masked>/g'
}
