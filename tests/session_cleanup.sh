#!/usr/bin/env bash
set -euo pipefail
base=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
# shellcheck source=src/lib/ollama.sh
source "$base/src/lib/ollama.sh"
state=$(mktemp)
log=$(mktemp "${TMPDIR:-/tmp}/opencode-local-ollama.XXXXXX.log")
trap 'rm -f "$state" "$log"' EXIT
bash -c 'exec -a "ollama serve" sleep 60' &
pid=$!
for _ in {1..50}; do
 if [[ $(ps -p "$pid" -o args= 2>/dev/null) == *'ollama serve'* ]]; then break; fi
 sleep 0.01
done
printf '%s\n%s\n%s\n' "$pid" "$log" '/tmp/models/example-4B' > "$state"
stop_managed_session "$state"
wait "$pid" 2>/dev/null || true
[[ ! -s $state && ! -e $log ]]
