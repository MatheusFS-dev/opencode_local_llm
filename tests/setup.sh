#!/usr/bin/env bash
set -euo pipefail
base=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir "$work/bin"
mkdir -p "$work/go/bin"
printf '#!/usr/bin/env bash\nprintf "go version go1.99.0 linux/amd64\\n"\n' > "$work/go/bin/go"
printf '#!/usr/bin/env bash\n' > "$work/go/bin/gofmt"
chmod +x "$work/go/bin/"*
cat > "$work/bin/curl" <<'MOCK'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$CALLS"
url=
while [[ $# -gt 0 ]]; do
 case $1 in
  -o) output=$2; shift 2 ;;
  *) url=$1; shift ;;
 esac
done
if [[ $url == 'https://go.dev/dl/?mode=json' ]]; then
 printf '%s\n' '[{"version":"go1.99.0","stable":true,"files":[{"os":"linux","arch":"amd64","kind":"archive","filename":"go1.99.0.linux-amd64.tar.gz","sha256":"test-sha"}]}]' > "$output"
else
 printf '#!/usr/bin/env bash\nprintf "installer ran\\n" >> "$CALLS"\n' > "$output"
fi
MOCK
cat > "$work/bin/sudo" <<'MOCK'
#!/usr/bin/env bash
printf 'sudo %s\n' "$*" >> "$CALLS"
MOCK
cat > "$work/bin/systemctl" <<'MOCK'
#!/usr/bin/env bash
case $1 in
 is-active|is-enabled) exit 0 ;;
 *) exit 0 ;;
esac
MOCK
chmod +x "$work/bin/"*
export CALLS="$work/calls"
printf '2\n' | HOME="$work/home" XDG_CONFIG_HOME="$work/config" GO_INSTALL_DIR="$work/go" GO_BIN_DIR="$work/go-links" PATH="$work/bin:$PATH" bash "$base/src/setup.sh" > "$work/output"
rg -q 'https://opencode.ai/v2/install' "$CALLS"
if rg -q 'sudo npm|nodejs npm' "$CALLS"; then
 exit 1
fi
[[ $(rg -c 'installer ran' "$CALLS") == 2 ]]
[[ $(cat "$work/config/opencode-local-llm/mode") == ondemand ]]
[[ $(cat "$work/config/opencode-local-llm/context-length") == 32768 ]]
rg -q '^sudo systemctl stop ollama$' "$CALLS"
rg -q '^sudo systemctl disable ollama$' "$CALLS"
