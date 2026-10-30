#!/usr/bin/env bash
# Runs the run's genesis binary like a user, from the run's spec directory,
# and saves the transcript (command, stdout, stderr, exit code) as evidence.
#   scripts/g.sh <run-id> <genesis arguments...>
# Set G_CWD to run from another directory (e.g. a worktree for `modules scaffold`).
# The exit code is genesis's own.
source "$(dirname "$0")/common.sh"
run_paths "${1:-}"; shift
[ -x "$RUN/bin/genesis" ] || { echo "no run $RUN_ID: run scripts/launch.sh first" >&2; exit 2; }
mkdir -p "$EVIDENCE"

n=$(find "$EVIDENCE" -name '[0-9][0-9]-*.txt' | wc -l | tr -d ' ')
slug=$(for a in "$@"; do basename -- "$a"; done | tr -c 'a-zA-Z0-9' '-' | tr -s '-' | cut -c1-40 | sed 's/^-//; s/-$//')
out="$EVIDENCE/$(printf '%02d' $((n + 1)))-$slug.txt"
tmp=$(mktemp -d)

set +e
(cd "${G_CWD:-$RUN/env}" && HOME="$RUN/home" NO_COLOR=1 "$RUN/bin/genesis" "$@" >"$tmp/out" 2>"$tmp/err")
code=$?
set -e

# `secrets get` prints a secret value: keep only its length.
if [ "${1:-}" = secrets ] && [ "${2:-}" = get ] && [ $code -eq 0 ]; then
	printf '<secret value masked, %s bytes>\n' "$(wc -c <"$tmp/out" | tr -d ' ')" >"$tmp/out"
fi

{
	echo "\$ genesis $*"
	echo "# cwd: ${G_CWD:-$RUN/env}  run: $RUN_ID  commit: $(cat "$RUN/build.txt")"
	echo "--- stdout"; mask <"$tmp/out"
	echo "--- stderr"; mask <"$tmp/err"
	echo "--- exit code: $code"
} >"$out"
rm -rf "$tmp"
cat "$out"
echo "(saved: $out)"
exit $code
