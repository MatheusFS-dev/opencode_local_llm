#!/usr/bin/env bash
set -euo pipefail
base=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
export HOME="$work/home" XDG_CONFIG_HOME="$work/config"
# shellcheck source=src/lib/ollama.sh
source "$base/src/lib/ollama.sh"
[[ -z $BASE_MODELS_DIR ]]
if prompt_models_dir <<< ''; then
 echo 'Empty first-time path unexpectedly accepted' >&2
 exit 1
fi
chosen="$work/custom models"
prompt_models_dir <<< "$chosen"
[[ $BASE_MODELS_DIR == "$chosen" ]]
[[ $(model_store_dir "$chosen" 'huihui_ai/example:4B') == "$chosen/huihui_ai/example-4B" ]]
if model_store_dir "$chosen" 'team/../other' >/dev/null 2>&1; then
 echo 'Traversal model name unexpectedly accepted' >&2
 exit 1
fi
write_base_dir
[[ $(cat "$BASE_MODELS_DIR_FILE") == "$chosen" ]]
# shellcheck source=src/lib/ollama.sh
source "$base/src/lib/ollama.sh"
prompt_models_dir <<< ''
[[ $BASE_MODELS_DIR == "$chosen" ]]
if prompt_models_dir <<< relative; then
 echo 'Relative path unexpectedly accepted' >&2
 exit 1
fi
