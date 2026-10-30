#!/usr/bin/env bash
# Tears down a run: `genesis destroy` and `genesis clean` through the CLI, then
# removes the genesis-* containers this run created (never one that existed
# before launch), then the run directory. Evidence is kept.
#   scripts/cleanup.sh <run-id>
source "$(dirname "$0")/common.sh"
run_paths "${1:-}"
[ -d "$RUN" ] || { echo "no run $RUN_ID in $VERIFY_ROOT/runs"; exit 0; }

if [ -f "$RUN/env/state/master.key" ]; then
	"$(dirname "$0")/g.sh" "$RUN_ID" destroy -f spec.yaml --auto-approve >/dev/null || echo "genesis destroy failed (transcript kept)"
	"$(dirname "$0")/g.sh" "$RUN_ID" clean -f spec.yaml --auto-approve >/dev/null || echo "genesis clean failed (transcript kept)"
fi

# Leftovers after destroy are a finding (see features/apply-destroy.md), record them.
docker ps -a --format '{{.Names}}' | sort >"$RUN/containers.after"
leftover=$(comm -13 "$RUN/containers.before" "$RUN/containers.after" | grep '^genesis-' || true)
if [ -n "$leftover" ]; then
	echo "$leftover" >"$EVIDENCE/leftover-containers-after-destroy.txt"
	echo "removing containers created by this run: $(echo "$leftover" | tr '\n' ' ')"
	echo "$leftover" | xargs docker rm -f >/dev/null
fi

rm -rf "$RUN"
echo "run $RUN_ID removed; evidence kept in $EVIDENCE"
ls "$EVIDENCE"
