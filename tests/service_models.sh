#!/usr/bin/env bash
set -euo pipefail
base=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin"
printf '#!/usr/bin/env bash\n' > "$work/bin/ollama"
printf '#!/usr/bin/env bash\necho systemd\n' > "$work/bin/ps"
printf '#!/usr/bin/env bash\n' > "$work/bin/systemctl"
cat > "$work/bin/sudo" <<'MOCK'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$CALLS"
if [[ $1 == tee ]]; then cat > "$DROPIN_OUTPUT"; fi
MOCK
chmod +x "$work/bin/"*
export CALLS="$work/calls"
export DROPIN_OUTPUT="$work/dropin"
chosen="$work/my models"
mkdir -p "$work/config/opencode-local-llm"
printf 'persistent\n' > "$work/config/opencode-local-llm/mode"
HOME="$work/home" XDG_CONFIG_HOME="$work/config" PATH="$work/bin:$PATH" bash "$base/src/service.sh" activate "$chosen" 'huihui_ai/example:4B' "$work/state" >/dev/null
[[ $(cat "$work/config/opencode-local-llm/models-dir") == "$chosen/huihui_ai/example-4B" ]]
[[ $(cat "$work/config/opencode-local-llm/models-base") == "$chosen" ]]
rg -Fq "OLLAMA_MODELS=$chosen/huihui_ai/example-4B" "$DROPIN_OUTPUT"
rg -q '^systemctl restart ollama$' "$CALLS"
