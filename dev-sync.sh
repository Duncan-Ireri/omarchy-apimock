#!/usr/bin/env bash
# Build, then copy the plugin into ~/.config/omarchy/plugins/ireri.apimock/ and
# ask the running shell to rescan. Run this after editing QML or Go while
# developing. (For a real install, `omarchy plugin add <git-url>` once this repo
# is pushed.)
set -euo pipefail

plugin_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
dest="$HOME/.config/omarchy/plugins/ireri.apimock"

"$plugin_dir/build.sh"

rm -rf "$dest"
mkdir -p "$dest/bin"
cp "$plugin_dir"/manifest.json "$plugin_dir"/Model.js "$plugin_dir"/*.qml "$dest/"
cp "$plugin_dir"/bin/omock "$dest/bin/"
for f in README.md LICENSE; do
  [[ -f "$plugin_dir/$f" ]] && cp "$plugin_dir/$f" "$dest/"
done

echo "synced -> $dest"

if command -v omarchy >/dev/null 2>&1; then
  omarchy plugin validate "$dest"
fi
if command -v omarchy-shell >/dev/null 2>&1; then
  omarchy-shell shell rescanPlugins || true
  echo "asked omarchy-shell to rescan plugins"
fi
