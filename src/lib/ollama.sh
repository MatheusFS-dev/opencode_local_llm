#!/usr/bin/env bash
MODE_FILE=${XDG_CONFIG_HOME:-$HOME/.config}/opencode-local-llm/mode
MODELS_DIR_FILE=${XDG_CONFIG_HOME:-$HOME/.config}/opencode-local-llm/models-dir
BASE_MODELS_DIR_FILE=${XDG_CONFIG_HOME:-$HOME/.config}/opencode-local-llm/models-base
CONTEXT_LENGTH_FILE=${XDG_CONFIG_HOME:-$HOME/.config}/opencode-local-llm/context-length
BASE_MODELS_DIR=${OLLAMA_MODELS_BASE:-$(cat "$BASE_MODELS_DIR_FILE" 2>/dev/null || cat "$MODELS_DIR_FILE" 2>/dev/null || true)}
MODELS_DIR=${OLLAMA_MODELS_DIR:-$(cat "$MODELS_DIR_FILE" 2>/dev/null || true)}
saved_context_length=$(cat "$CONTEXT_LENGTH_FILE" 2>/dev/null || true)
case $saved_context_length in 16384|32768) ;; *) saved_context_length=16384 ;; esac
export DROPIN=/etc/systemd/system/ollama.service.d/local-models.conf
export OLLAMA_CONTEXT_LENGTH=${OLLAMA_CONTEXT_LENGTH:-$saved_context_length} OLLAMA_HOST=127.0.0.1:11434 OLLAMA_NO_CLOUD=1
if [[ -n $MODELS_DIR ]]; then export OLLAMA_MODELS="$MODELS_DIR"; fi
fail() { echo "$*" >&2; return 1; }
require_models_dir() { [[ $MODELS_DIR == /* ]] || fail 'Choose an absolute model directory with src/download.sh or set OLLAMA_MODELS_DIR.'; }
require_ollama() { command -v ollama >/dev/null || fail 'Ollama is missing. Run src/setup.sh.'; }
port_state() {
 if curl -fsS --max-time 2 http://127.0.0.1:11434/api/version >/dev/null 2>&1; then echo ollama
 elif (echo >/dev/tcp/127.0.0.1/11434) >/dev/null 2>&1; then echo occupied
 else echo free; fi
}
mode() { cat "$MODE_FILE" 2>/dev/null || echo ondemand; }
write_mode() { mkdir -p "$(dirname "$MODE_FILE")"; printf '%s\n' "$1" > "$MODE_FILE"; }
write_models_dir() { require_models_dir || return; mkdir -p "$(dirname "$MODELS_DIR_FILE")"; printf '%s\n' "$MODELS_DIR" > "$MODELS_DIR_FILE"; }
write_base_dir() { [[ $BASE_MODELS_DIR == /* ]] || { fail 'Model base directory must be absolute.'; return 1; }; mkdir -p "$(dirname "$BASE_MODELS_DIR_FILE")"; printf '%s\n' "$BASE_MODELS_DIR" > "$BASE_MODELS_DIR_FILE"; }
write_context_length() { mkdir -p "$(dirname "$CONTEXT_LENGTH_FILE")"; printf '%s\n' "$OLLAMA_CONTEXT_LENGTH" > "$CONTEXT_LENGTH_FILE"; }
prompt_context_length() {
 local choice
 printf 'Context window:\n1) 16K tokens\n2) 32K tokens\nChoice [1]: ' >&2
 if ! IFS= read -r choice; then choice=1; fi
 case ${choice:-1} in
  1|16|16k|16K) OLLAMA_CONTEXT_LENGTH=16384 ;;
  2|32|32k|32K) OLLAMA_CONTEXT_LENGTH=32768 ;;
  *) fail 'Choose 1 for 16K or 2 for 32K.'; return 1 ;;
 esac
 export OLLAMA_CONTEXT_LENGTH
 write_context_length
}
model_store_dir() {
 local base=$1 model=$2 folder
 [[ $base == /* ]] || { fail 'Model base directory must be absolute.'; return 1; }
 [[ $model =~ ^[A-Za-z0-9][A-Za-z0-9._/-]*(:[A-Za-z0-9._-]+)?$ ]] || { fail "Invalid model name: $model"; return 1; }
 [[ ! $model =~ (^|/)\.\.?(/|$) ]] || { fail "Invalid model name: $model"; return 1; }
 folder=${model//:/-}
 printf '%s/%s\n' "${base%/}" "$folder"
}
prompt_models_dir() {
 local answer
 printf 'Each model uses <base directory>/<model name>.\n' >&2
 if [[ -n $BASE_MODELS_DIR ]]; then printf 'Base directory [%s]: ' "$BASE_MODELS_DIR" >&2
 else printf 'Base directory: ' >&2; fi
 if ! IFS= read -r answer; then fail 'No model directory was entered.'; return 1; fi
 answer=${answer:-$BASE_MODELS_DIR}
 [[ $answer == /* ]] || { fail 'Model directory must be an absolute path.'; return 1; }
 BASE_MODELS_DIR=$answer
 export OLLAMA_MODELS_BASE="$answer"
}
prepare_dir() { require_models_dir || return; mkdir -p "$MODELS_DIR"; }
stop_session() {
 if [[ -n ${SERVER_PID:-} ]]; then kill "$SERVER_PID" 2>/dev/null || true; wait "$SERVER_PID" 2>/dev/null || true; unset SERVER_PID; fi
 if [[ -n ${SERVER_LOG:-} ]]; then rm -f "$SERVER_LOG"; unset SERVER_LOG; fi
}
stop_managed_session() {
 local state_file=$1 pid log
 if [[ -s $state_file ]]; then
  { IFS= read -r pid; IFS= read -r log; } < "$state_file"
  if [[ $pid =~ ^[0-9]+$ ]] && [[ $(ps -p "$pid" -o args= 2>/dev/null) == *'ollama serve'* ]]; then
   kill "$pid" 2>/dev/null || true
   for _ in {1..50}; do
    kill -0 "$pid" 2>/dev/null || break
    sleep 0.1
   done
  fi
  if [[ $log == "${TMPDIR:-/tmp}/opencode-local-ollama."* ]]; then rm -f "$log"; fi
  : > "$state_file"
 fi
}
start_session() {
 require_ollama || return
 case $(port_state) in
  ollama) fail 'An Ollama server is already running. Its model path and context cannot be verified.'; return ;;
  occupied) fail 'Port 11434 is occupied by another process.'; return ;;
 esac
 prepare_dir
 SERVER_LOG=$(mktemp "${TMPDIR:-/tmp}/opencode-local-ollama.XXXXXX.log")
 ollama serve >"$SERVER_LOG" 2>&1 & SERVER_PID=$!
 for _ in {1..50}; do
  if [[ $(port_state) == ollama ]]; then return 0; fi
  if ! kill -0 "$SERVER_PID" 2>/dev/null; then break; fi
  sleep 0.2
 done
 if [[ -n ${SERVER_PID:-} ]]; then kill "$SERVER_PID" 2>/dev/null || true; wait "$SERVER_PID" 2>/dev/null || true; unset SERVER_PID; fi
 fail "Ollama did not start. See $SERVER_LOG"
}
ensure_service() {
 require_models_dir || return
 require_ollama || return
 if [[ $(mode) == persistent ]]; then
  [[ $(port_state) == ollama ]] || fail 'Persistent Ollama service is unavailable. Check systemctl status ollama.'
 else
  start_session
 fi
}
