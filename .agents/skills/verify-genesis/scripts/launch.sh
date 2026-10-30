#!/usr/bin/env bash
# Prepares a disposable Genesis install: builds the CLI from this checkout,
# installs every module of modules/ with `genesis modules install`, writes the
# fake-compute spec and runs `genesis init`. Prints the run id on its last line.
#   scripts/launch.sh [run-id]
source "$(dirname "$0")/common.sh"
run_paths "${1:-$(date +%Y%m%d-%H%M%S)}"
[ ! -e "$RUN" ] || { echo "run $RUN_ID already exists: pick another id or clean it up" >&2; exit 2; }
docker info >/dev/null 2>&1 || { echo "docker daemon unreachable" >&2; exit 2; }

mkdir -p "$RUN"/{bin,home,env} "$EVIDENCE"
# Containers that existed before this run: cleanup never touches them.
docker ps -a --format '{{.Names}}' | sort >"$RUN/containers.before"

echo "==> building genesis from $REPO"
(cd "$REPO" && go build -o "$RUN/bin/genesis" ./cmd/genesis)
git -C "$REPO" rev-parse --short HEAD >"$RUN/build.txt"
git -C "$REPO" diff --quiet HEAD || echo "dirty" >>"$RUN/build.txt"

sed "s|@STATE_DIR@|$RUN/env/state|" "$(dirname "$0")/../assets/spec.yaml" >"$RUN/env/spec.yaml"

for m in "$REPO"/modules/*/; do
	"$(dirname "$0")/g.sh" "$RUN_ID" modules install "$m" >/dev/null ||
		{ echo "installing $m failed: see $EVIDENCE" >&2; exit 1; }
done
"$(dirname "$0")/g.sh" "$RUN_ID" init -f spec.yaml >/dev/null ||
	{ echo "genesis init failed: see $EVIDENCE" >&2; exit 1; }

"$(dirname "$0")/doctor.sh" "$RUN_ID"
echo "$RUN_ID"
