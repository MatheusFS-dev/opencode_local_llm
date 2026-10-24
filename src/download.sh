#!/usr/bin/env bash
set -euo pipefail
BASE=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=src/lib/ollama.sh
source "$BASE/lib/ollama.sh"
prompt_models_dir
write_base_dir
for tool in curl python3 sha256sum tar dpkg; do command -v "$tool" >/dev/null || fail "$tool is required"; done
ARCH=$(dpkg --print-architecture)
case $ARCH in amd64|arm64) ;; *) fail "Unsupported architecture: $ARCH"; exit 1 ;; esac
META=$(mktemp)
ARCHIVE=$(mktemp)
STATE_FILE=$(mktemp)
trap 'stop_managed_session "$STATE_FILE"; rm -f "$META" "$ARCHIVE" "$STATE_FILE"' EXIT
curl -fsSL 'https://go.dev/dl/?mode=json' -o "$META"
read -r VERSION FILE SHA < <(python3 - "$META" "$ARCH" <<'PYGO'
import json
import sys
with open(sys.argv[1], encoding='utf-8') as source:
    releases = json.load(source)
for release in releases:
    if release['stable']:
        for file in release['files']:
            if file['os'] == 'linux' and file['arch'] == sys.argv[2] and file['kind'] == 'archive':
                print(release['version'], file['filename'], file['sha256'])
                sys.exit(0)
raise SystemExit('No stable Go archive found')
PYGO
)
[[ -n $VERSION && -n $FILE && -n $SHA ]] || fail 'Invalid Go release metadata'
if [[ ! -x /usr/local/go/bin/go || $(/usr/local/go/bin/go version) != *"$VERSION "* ]]; then
 curl -fsSL "https://go.dev/dl/$FILE" -o "$ARCHIVE"
 printf '%s  %s\n' "$SHA" "$ARCHIVE" | sha256sum -c -
 sudo rm -rf /usr/local/go
 sudo tar -C /usr/local -xzf "$ARCHIVE"
fi
export PATH="/usr/local/go/bin:$PATH"
(cd "$BASE/downloader" && go build -o "$BASE/downloader/local-downloader" .)
"$BASE/downloader/local-downloader" "$BASE/models.json" "$BASE_MODELS_DIR" "$BASE/service.sh" "$STATE_FILE" "$(mode)"
