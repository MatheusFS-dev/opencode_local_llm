#!/usr/bin/env bash
set -euo pipefail
trap 'echo "Installation failed at: $BASH_COMMAND" >&2' ERR
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
installer=$(mktemp)
trap 'rm -f "$installer"' EXIT
curl -fsSL https://ollama.com/install.sh -o "$installer"
sh "$installer"
curl -fsSL https://opencode.ai/v2/install -o "$installer"
bash "$installer"
echo 'Ollama and OpenCode V2 installed. Select a mode with src/service.sh.'
