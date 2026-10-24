#!/usr/bin/env bash
set -euo pipefail
BASE=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=src/lib/ollama.sh
source "$BASE/lib/ollama.sh"
case ${1:-} in
 ondemand)
  if command -v systemctl >/dev/null; then
   if systemctl is-active --quiet ollama 2>/dev/null; then sudo systemctl stop ollama; fi
   if systemctl is-enabled --quiet ollama 2>/dev/null; then sudo systemctl disable ollama; fi
  fi
  write_mode ondemand
  echo 'On-demand mode selected.' ;;
 persistent)
  require_ollama
  command -v systemctl >/dev/null || fail 'systemd is required.'
  [[ $(ps -p 1 -o comm=) == systemd ]] || fail 'systemd is not PID 1. Enable it in /etc/wsl.conf and restart WSL.'
  prepare_dir
  sudo mkdir -p "$(dirname "$DROPIN")"
  unit_models_dir=${MODELS_DIR//\\/\\\\}
  unit_models_dir=${unit_models_dir//\"/\\\"}
  unit_models_dir=${unit_models_dir//%/%%}
  printf '[Service]\nEnvironment="OLLAMA_MODELS=%s"\nEnvironment="OLLAMA_CONTEXT_LENGTH=16384"\nEnvironment="OLLAMA_HOST=127.0.0.1:11434"\nEnvironment="OLLAMA_NO_CLOUD=1"\n' "$unit_models_dir" | sudo tee "$DROPIN" >/dev/null
  sudo chown ollama:"$(id -gn)" "$MODELS_DIR"
  sudo chmod 2775 "$MODELS_DIR"
  sudo systemctl daemon-reload
  sudo systemctl enable ollama
  sudo systemctl restart ollama
  write_models_dir
  write_mode persistent
  echo 'Persistent mode selected.' ;;
 activate)
  [[ $# == 4 ]] || fail 'Usage: service.sh activate BASE_DIR MODEL STATE_FILE'
  selected_base=$2
  selected_model=$3
  state_file=$4
  selected_root=$(model_store_dir "$selected_base" "$selected_model")
  BASE_MODELS_DIR=$selected_base
  export OLLAMA_MODELS_BASE="$selected_base" OLLAMA_MODELS_DIR="$selected_root"
  if [[ $(mode) == persistent ]]; then
   "$BASE/service.sh" persistent
  else
   stop_managed_session "$state_file"
   MODELS_DIR=$selected_root
   export OLLAMA_MODELS="$selected_root"
   start_session
   printf '%s\n%s\n%s\n' "$SERVER_PID" "$SERVER_LOG" "$selected_root" > "$state_file"
   write_models_dir
  fi
  write_base_dir ;;
 status) printf 'Mode: %s\nModels: %s\nPort: %s\n' "$(mode)" "${MODELS_DIR:-(not configured)}" "$(port_state)" ;;
 *) echo 'Usage: service.sh {ondemand|persistent|status}' >&2; exit 2 ;;
esac
