#!/bin/sh
# Sets the plugin version in both manifests, and with -b also VERSION (the
# godon binary the launchers download — only point it at a released tag).
#
#   scripts/bump.sh 0.1.2       plugin manifests only
#   scripts/bump.sh -b 0.1.2    plugin manifests and VERSION
set -eu

binary=0
if [ "${1:-}" = "-b" ]; then
  binary=1
  shift
fi

version=${1:-}
case $version in
  [0-9]*.[0-9]*.[0-9]*) ;;
  *)
    echo "usage: scripts/bump.sh [-b] X.Y.Z" >&2
    exit 2
    ;;
esac

root=$(cd "$(dirname "$0")/.." && pwd)

# Rewrite only the "version" line so the rest of the file keeps its layout.
# A temp file instead of sed -i, whose syntax differs between GNU and BSD.
for f in "$root/.claude-plugin/plugin.json" "$root/plugin.json"; do
  sed "s/^\(  \"version\": \"\)[^\"]*\"/\1$version\"/" "$f" >"$f.tmp"
  if ! grep -q "\"version\": \"$version\"" "$f.tmp"; then
    rm -f "$f.tmp"
    echo "bump: no \"version\" line found in $f" >&2
    exit 1
  fi
  mv "$f.tmp" "$f"
  echo "$f -> $version"
done

if [ "$binary" = 1 ]; then
  echo "$version" >"$root/VERSION"
  echo "$root/VERSION -> $version"
fi
