#!/usr/bin/env bash
set -euo pipefail

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
tools="$work/tools"
install_dir="$work/go"
bin_dir="$work/bin"
mkdir -p "$tools"

cat > "$tools/curl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
output=
url=
while (($#)); do
 case $1 in
  -o) output=$2; shift 2 ;;
  *) url=$1; shift ;;
 esac
done
if [[ $url == *'?mode=json' ]]; then
 printf '%s\n' '[{"version":"go1.99.0","stable":true,"files":[{"os":"linux","arch":"amd64","kind":"archive","filename":"go1.99.0.linux-amd64.tar.gz","sha256":"test-sha"}]}]' > "$output"
else
 printf 'archive' > "$output"
fi
EOF
cat > "$tools/dpkg" <<'EOF'
#!/usr/bin/env bash
printf 'amd64\n'
EOF
cat > "$tools/sha256sum" <<'EOF'
#!/usr/bin/env bash
cat >/dev/null
EOF
cat > "$tools/sudo" <<'EOF'
#!/usr/bin/env bash
exec "$@"
EOF
cat > "$tools/tar" <<'EOF'
#!/usr/bin/env bash
mkdir -p "$GO_INSTALL_DIR/bin"
printf '#!/usr/bin/env bash\nprintf "go version go1.99.0 linux/amd64\\n"\n' > "$GO_INSTALL_DIR/bin/go"
chmod +x "$GO_INSTALL_DIR/bin/go"
printf '#!/usr/bin/env bash\n' > "$GO_INSTALL_DIR/bin/gofmt"
chmod +x "$GO_INSTALL_DIR/bin/gofmt"
EOF
chmod +x "$tools"/*

export GO_INSTALL_DIR="$install_dir"
export GO_BIN_DIR="$bin_dir"
PATH="$tools:$PATH"
# shellcheck source=src/lib/go.sh
source "$root/src/lib/go.sh"
ensure_go

[[ -x $install_dir/bin/go ]]
[[ $("$install_dir/bin/go" version) == 'go version go1.99.0 linux/amd64' ]]
if [[ ! -L $bin_dir/go || ! -L $bin_dir/gofmt ]]; then
 echo 'Go command links were not created' >&2
 exit 1
fi
[[ $PATH == "$install_dir/bin:"* ]]
