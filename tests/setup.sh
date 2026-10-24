#!/usr/bin/env bash
set -euo pipefail
base=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir "$work/bin"
cat > "$work/bin/curl" <<'MOCK'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$CALLS"
while [[ $# -gt 0 ]]; do
 case $1 in
  -o) output=$2; shift 2 ;;
  *) shift ;;
 esac
done
printf '#!/usr/bin/env bash\nprintf "installer ran\\n" >> "$CALLS"\n' > "$output"
MOCK
cat > "$work/bin/sudo" <<'MOCK'
#!/usr/bin/env bash
printf 'sudo %s\n' "$*" >> "$CALLS"
MOCK
chmod +x "$work/bin/"*
export CALLS="$work/calls"
PATH="$work/bin:$PATH" bash "$base/src/setup.sh" > "$work/output"
rg -q 'https://opencode.ai/v2/install' "$CALLS"
if rg -q 'sudo npm|nodejs npm' "$CALLS"; then
 exit 1
fi
[[ $(rg -c 'installer ran' "$CALLS") == 2 ]]
