#!/usr/bin/env bash
set -euo pipefail
BASE=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=src/lib/ollama.sh
source "$BASE/lib/ollama.sh"
export PATH="$HOME/.opencode/bin:$PATH"
command -v opencode >/dev/null || fail 'OpenCode is missing. Run src/setup.sh.'
project=${1:-$PWD}
[[ -d $project ]] || fail "Project directory does not exist: $project"
trap stop_session EXIT
ensure_service
(cd "$project" && opencode)
