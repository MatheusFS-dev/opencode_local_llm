#!/usr/bin/env bash
set -euo pipefail
base=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/home/.opencode/bin" "$work/config/opencode-local-llm" "$work/bin"
printf 'persistent\n' > "$work/config/opencode-local-llm/mode"
printf '%s\n' "$work/models" > "$work/config/opencode-local-llm/models-dir"
printf '#!/usr/bin/env bash\nprintf "launched\\n"\n' > "$work/home/.opencode/bin/opencode"
printf '#!/usr/bin/env bash\n' > "$work/bin/ollama"
printf '#!/usr/bin/env bash\nexit 0\n' > "$work/bin/curl"
chmod +x "$work/home/.opencode/bin/opencode" "$work/bin/"*
HOME="$work/home" XDG_CONFIG_HOME="$work/config" PATH="$work/bin:/usr/bin:/bin" bash "$base/src/run.sh" "$work" | rg -q '^launched$'
