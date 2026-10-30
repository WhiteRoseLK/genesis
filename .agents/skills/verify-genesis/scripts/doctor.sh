#!/usr/bin/env bash
# Read-only: is this run worth driving? Exit 0 when every check passes.
#   scripts/doctor.sh <run-id>
source "$(dirname "$0")/common.sh"
run_paths "${1:-}"
fail=0
ok()  { echo "ok    $*"; }
bad() { echo "FAIL  $*"; fail=1; }

if [ -x "$RUN/bin/genesis" ]; then
	ok "binary: $("$RUN/bin/genesis" --version)"
else
	bad "no binary in $RUN/bin (run scripts/launch.sh)"
fi

head=$(git -C "$REPO" rev-parse --short HEAD)
built=$(head -1 "$RUN/build.txt" 2>/dev/null || echo none)
if [ "$built" = "$head" ]; then
	dirty=""
	if grep -q dirty "$RUN/build.txt"; then dirty=" (dirty tree)"; fi
	ok "built from HEAD $head$dirty"
else
	bad "built from $built, checkout is at $head: launch a new run"
fi

want=$(find "$REPO/modules" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ')
got=$(HOME="$RUN/home" "$RUN/bin/genesis" modules list 2>/dev/null | wc -l | tr -d ' ')
if [ "$got" = "$want" ]; then ok "modules installed: $got/$want"; else bad "modules installed: $got/$want"; fi

if [ -f "$RUN/env/state/master.key" ]; then
	ok "state_dir initialised: $RUN/env/state"
else
	bad "no master key in $RUN/env/state (genesis init)"
fi

if docker info >/dev/null 2>&1; then
	ok "docker daemon reachable"
	foreign=$(grep '^genesis-' "$RUN/containers.before" 2>/dev/null | tr '\n' ' ' || true)
	if [ -z "$foreign" ]; then
		ok "no genesis-* container from outside this run"
	else
		bad "genesis-* containers predate this run (names would collide, not ours to touch): $foreign"
	fi
else
	bad "docker daemon unreachable"
fi
exit $fail
