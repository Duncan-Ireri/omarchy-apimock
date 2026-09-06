#!/usr/bin/env bash
# Build the omock helper from source into bin/, where Service.qml looks for it.
set -euo pipefail

plugin_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
version="$(sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$plugin_dir/manifest.json" | head -1)"
version="${version:-dev}"

# -trimpath + -buildvcs=false + CGO_ENABLED=0 + the pinned toolchain (go.mod)
# make this build reproducible: the same source produces the same bytes on any
# machine, so anyone can rebuild and compare against checksums/.
(
  cd "$plugin_dir/backend"
  CGO_ENABLED=0 go build -trimpath -buildvcs=false \
    -ldflags "-s -w -X main.version=${version}" \
    -o "$plugin_dir/bin/omock" .
)

echo "built $plugin_dir/bin/omock ($("$plugin_dir/bin/omock" --version))"
