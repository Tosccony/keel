#!/usr/bin/env bash
# Builds the Linux binary and packages it as .deb, .rpm and .tar.gz in dist/.
# Run on Linux x86_64 with nfpm on PATH.
# Usage: scripts/package-linux.sh <version>
set -euo pipefail

VERSION="${1:-0.0.0-dev}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DIST="$ROOT/dist"

rm -rf "$DIST/linux"
mkdir -p "$DIST/linux"

CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -trimpath \
  -ldflags "-s -w -X main.version=$VERSION" \
  -o "$DIST/linux/keel" "$ROOT/cmd/keel"

# The runtimes Keel ships (scripts/fetch-runtimes.sh linux-x86_64 ...).
RUNTIMES="$DIST/runtimes/linux-x86_64"
for part in mariadb apache php phpmyadmin; do
  if [ ! -d "$RUNTIMES/$part" ]; then
    echo "package-linux: $RUNTIMES/$part is missing; run scripts/fetch-runtimes.sh linux-x86_64 $RUNTIMES" >&2
    exit 1
  fi
done

export VERSION
cd "$ROOT"
nfpm package --config packaging/linux/nfpm.yaml --packager deb --target "$DIST/keel_amd64.deb"
nfpm package --config packaging/linux/nfpm.yaml --packager rpm --target "$DIST/keel_x86_64.rpm"

# Plain archive for distros without apt, dnf or zypper.
tar -czf "$DIST/keel_linux_x86_64.tar.gz" \
  -C "$DIST/linux" keel \
  -C "$ROOT/packaging/linux" keel.desktop keel.png \
  -C "$RUNTIMES" mariadb apache php phpmyadmin

echo "Built $DIST/keel_amd64.deb, keel_x86_64.rpm, keel_linux_x86_64.tar.gz"
