#!/usr/bin/env bash
set -euo pipefail
BASE=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=src/lib/ollama.sh
source "$BASE/lib/ollama.sh"
export PATH="$HOME/.opencode/bin:$PATH"
command -v opencode >/dev/null || fail 'OpenCode is missing. Run src/setup.sh.'
command -v python3 >/dev/null || fail 'python3 is required.'
opencode_port_available() {
 python3 - "$1" <<'PY'
import socket
import sys

with socket.socket() as sock:
    try:
        sock.bind(("127.0.0.1", int(sys.argv[1])))
    except OSError:
        raise SystemExit(1)
PY
}
configure_opencode_service_port() {
 local port candidate
 if [[ $(opencode service status 2>/dev/null) == running ]]; then return; fi
 port=$(opencode service get port 2>/dev/null || true)
 [[ $port =~ ^[0-9]+$ ]] || port=49374
 if opencode_port_available "$port"; then return; fi
 for ((candidate = port + 1; candidate <= port + 100; candidate++)); do
  if opencode_port_available "$candidate"; then
   opencode service set port "$candidate"
   printf 'OpenCode service port %s is occupied. Using %s.\n' "$port" "$candidate" >&2
   return
  fi
 done
 fail "No free OpenCode service port found near $port."
}
configure_opencode_service_port
project=${1:-$PWD}
[[ -d $project ]] || fail "Project directory does not exist: $project"
trap stop_session EXIT
ensure_service
(cd "$project" && opencode)
