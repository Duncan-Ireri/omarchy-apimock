#!/usr/bin/env bash
# Install the omock helper matching this plugin's version.
#
# The prebuilt binary is fetched from the GitHub release, then verified against
# the SHA-256 committed in this repo at checksums/ — NOT against a checksum
# downloaded alongside the binary. The release is mutable; a checksum served from
# the same place is not an independent check. The pinned digest ships with the
# reviewed source you just cloned, and the build is reproducible: run ./build.sh
# on any machine with the Go toolchain and you get the exact same bytes.
#
# Run this once after `omarchy plugin add https://github.com/Duncan-Ireri/omarchy-apimock`.
# On anything other than Linux x86_64, use ./build.sh instead (needs the Go toolchain).
set -euo pipefail

readonly repo="Duncan-Ireri/omarchy-apimock"
plugin_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

fail() {
  echo "apimock-install: $*" >&2
  exit 1
}

[[ "$(id -u)" != "0" ]] || fail "do not run as root — this plugin needs no privileges"

for cmd in curl jq sha256sum install uname; do
  command -v "$cmd" >/dev/null 2>&1 || fail "required command not found: $cmd"
done

[[ "$(uname -s)" == "Linux" ]] || fail "prebuilt helpers are Linux-only — run ./build.sh instead"
case "$(uname -m)" in
  x86_64 | amd64) ;;
  *) fail "no prebuilt helper for $(uname -m) — run ./build.sh instead (needs Go)" ;;
esac

version="$(jq -r '.version' "$plugin_dir/manifest.json")"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail "manifest.json version '$version' is not X.Y.Z"

readonly tag="v${version}"
readonly asset="omock-${tag}-linux-x86_64"
readonly base="https://github.com/${repo}/releases/download/${tag}"

# The trust anchor: a checksum committed to this repo, reviewed alongside the code.
readonly pinned="$plugin_dir/checksums/$asset.sha256"
[[ -f "$pinned" ]] ||
  fail "no pinned checksum at checksums/$asset.sha256 — build from source with ./build.sh instead"
read -r expected_digest _ < "$pinned"
[[ "$expected_digest" =~ ^[0-9a-f]{64}$ ]] ||
  fail "pinned checksum file is malformed: $pinned"

workdir="$(mktemp -d)"
# shellcheck disable=SC2064
trap "rm -rf '$workdir'" EXIT

curl --fail --location --silent --show-error --output "$workdir/$asset" "$base/$asset" ||
  fail "could not download $asset from release $tag"

printf '%s  %s\n' "$expected_digest" "$asset" > "$workdir/$asset.sha256"
( cd "$workdir" && sha256sum --check --status "$asset.sha256" ) ||
  fail "$asset does not match the pinned checksum — refusing to install (try ./build.sh)"

install -Dm755 "$workdir/$asset" "$plugin_dir/bin/omock"

installed="$("$plugin_dir/bin/omock" --version 2>/dev/null || true)"
[[ "$installed" == "omock ${version}" ]] ||
  fail "installed helper reports '$installed', expected 'omock ${version}'"

echo "apimock-install: installed omock ${version} -> $plugin_dir/bin/omock"
echo "apimock-install: now run 'omarchy plugin enable ireri.apimock' then 'omarchy restart shell'"
