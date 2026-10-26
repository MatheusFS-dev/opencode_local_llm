#!/usr/bin/env bash

link_go_command() {
 local source=$1 destination=$2
 if [[ -e $destination && ! -L $destination ]]; then
  echo "Refusing to replace non-symlink Go command: $destination" >&2
  return 1
 fi
 sudo ln -sfn "$source" "$destination"
}

ensure_go() {
 local install_dir=${GO_INSTALL_DIR:-/usr/local/go}
 local bin_dir=${GO_BIN_DIR:-/usr/local/bin}
 local go_bin="$install_dir/bin/go"
 local arch meta archive version file sha
 for tool in curl python3 sha256sum tar dpkg sudo mkdir ln; do
  command -v "$tool" >/dev/null || { echo "$tool is required" >&2; return 1; }
 done
 arch=$(dpkg --print-architecture)
 case $arch in amd64|arm64) ;; *) echo "Unsupported architecture: $arch" >&2; return 1 ;; esac
 meta=$(mktemp) || return 1
 archive=$(mktemp) || { rm -f "$meta"; return 1; }
 if ! curl -fsSL 'https://go.dev/dl/?mode=json' -o "$meta"; then
  rm -f "$meta" "$archive"
  return 1
 fi
 if ! read -r version file sha < <(python3 - "$meta" "$arch" <<'PYGO'
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
 ); then
  rm -f "$meta" "$archive"
  return 1
 fi
 [[ -n $version && -n $file && -n $sha ]] || { rm -f "$meta" "$archive"; echo 'Invalid Go release metadata' >&2; return 1; }
 if [[ ! -x $go_bin || $($go_bin version) != *"$version "* ]]; then
  if ! curl -fsSL "https://go.dev/dl/$file" -o "$archive" || ! printf '%s  %s\n' "$sha" "$archive" | sha256sum -c - || ! sudo rm -rf "$install_dir" || ! sudo tar -C "$(dirname "$install_dir")" -xzf "$archive"; then
   rm -f "$meta" "$archive"
   return 1
  fi
 fi
 if [[ ! -x $install_dir/bin/gofmt ]] || ! sudo mkdir -p "$bin_dir" || ! link_go_command "$go_bin" "$bin_dir/go" || ! link_go_command "$install_dir/bin/gofmt" "$bin_dir/gofmt"; then
  rm -f "$meta" "$archive"
  return 1
 fi
 rm -f "$meta" "$archive"
 export PATH="$install_dir/bin:$PATH"
}
