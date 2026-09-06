#!/usr/bin/env bash
# Download the checksum-verified omock helper matching this plugin's version.
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

workdir="$(mktemp -d)"
# shellcheck disable=SC2064
trap "rm -rf '$workdir'" EXIT

curl --fail --location --silent --show-error --output "$workdir/$asset" "$base/$asset" ||
  fail "could not download $asset from release $tag"
curl --fail --location --silent --show-error --output "$workdir/$asset.sha256" "$base/$asset.sha256" ||
  fail "could not download the checksum for $asset"

( cd "$workdir" && sha256sum --check --status "$asset.sha256" ) ||
  fail "checksum mismatch for $asset — refusing to install"

install -Dm755 "$workdir/$asset" "$plugin_dir/bin/omock"

installed="$("$plugin_dir/bin/omock" --version 2>/dev/null || true)"
[[ "$installed" == "omock ${version}" ]] ||
  fail "installed helper reports '$installed', expected 'omock ${version}'"

echo "apimock-install: installed omock ${version} -> $plugin_dir/bin/omock"
echo "apimock-install: now run 'omarchy plugin enable ireri.apimock' then 'omarchy restart shell'"
