#!/usr/bin/env bash
set -euo pipefail
base=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/home/.opencode/bin" "$work/config/opencode-local-llm" "$work/bin"
printf 'persistent\n' > "$work/config/opencode-local-llm/mode"
printf '%s\n' "$work/models" > "$work/config/opencode-local-llm/models-dir"
cat > "$work/home/.opencode/bin/opencode" <<'MOCK'
#!/usr/bin/env bash
if [[ ${1:-} == service ]]; then
 case ${2:-} in
  get) printf '49374\n' ;;
  set) printf 'service set %s\n' "$*" >> "$CALLS" ;;
 esac
 exit 0
fi
printf 'launched\n'
MOCK
printf '#!/usr/bin/env bash\n' > "$work/bin/ollama"
printf '#!/usr/bin/env bash\nexit 0\n' > "$work/bin/curl"
cat > "$work/bin/python3" <<'MOCK'
#!/usr/bin/env bash
if [[ ${!#} == 49374 ]]; then exit 1; fi
exit 0
MOCK
chmod +x "$work/home/.opencode/bin/opencode" "$work/bin/"*
export CALLS="$work/calls"
HOME="$work/home" XDG_CONFIG_HOME="$work/config" PATH="$work/bin:/usr/bin:/bin" bash "$base/src/run.sh" "$work" | rg -q '^launched$'
rg -q '^service set service set port 49375$' "$CALLS"
