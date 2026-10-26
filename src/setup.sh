#!/usr/bin/env bash
set -euo pipefail
trap 'echo "Installation failed at: $BASH_COMMAND" >&2' ERR
BASE=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=src/lib/go.sh
source "$BASE/lib/go.sh"
# shellcheck source=src/lib/ollama.sh
source "$BASE/lib/ollama.sh"
command -v curl >/dev/null
# shellcheck source=/etc/os-release
if [[ -f /etc/os-release ]]; then
 . /etc/os-release
fi
if [[ ${ID:-} == ubuntu ]]; then
 if ! command -v zstd >/dev/null; then
  sudo apt-get update
  sudo apt-get install -y zstd
 fi
fi
ensure_go
installer=$(mktemp)
trap 'rm -f "$installer"' EXIT
curl -fsSL https://ollama.com/install.sh -o "$installer"
sh "$installer"
curl -fsSL https://opencode.ai/v2/install -o "$installer"
bash "$installer"
prompt_context_length
"$BASE/service.sh" ondemand
echo 'Go, Ollama, and OpenCode V2 installed. Ollama is configured for on-demand mode.'
