#!/usr/bin/env bash
set -euo pipefail
BASE=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=src/lib/ollama.sh
source "$BASE/lib/ollama.sh"
# shellcheck source=src/lib/go.sh
source "$BASE/lib/go.sh"
prompt_models_dir
write_base_dir
STATE_FILE=$(mktemp)
trap 'stop_managed_session "$STATE_FILE"; rm -f "$STATE_FILE"' EXIT
ensure_go
(cd "$BASE/downloader" && go build -o "$BASE/downloader/local-downloader" .)
"$BASE/downloader/local-downloader" "$BASE/models.json" "$BASE_MODELS_DIR" "$BASE/service.sh" "$STATE_FILE" "$(mode)"
