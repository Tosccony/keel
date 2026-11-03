#!/usr/bin/env bash
# Builds a universal (Apple Silicon + Intel) Keel.app and wraps it in
# dist/Keel-macOS.dmg. Run on a Mac.
# Usage: scripts/package-macos.sh <version>
set -euo pipefail

VERSION="${1:-0.0.0-dev}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORK="$ROOT/dist/macos"
APP="$WORK/Keel.app"
DMG="$ROOT/dist/Keel-macOS.dmg"

export CGO_ENABLED=1
export MACOSX_DEPLOYMENT_TARGET="${MACOSX_DEPLOYMENT_TARGET:-12.0}"

rm -rf "$WORK" "$DMG"
mkdir -p "$WORK"

# One binary per CPU type, then glue them into a universal binary.
for arch in arm64 amd64; do
  GOOS=darwin GOARCH="$arch" go build -trimpath \
    -ldflags "-s -w -X main.version=$VERSION" \
    -o "$WORK/keel-$arch" "$ROOT/cmd/keel"
done

mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
lipo -create -output "$APP/Contents/MacOS/keel" "$WORK/keel-arm64" "$WORK/keel-amd64"
sed "s/__VERSION__/$VERSION/g" "$ROOT/packaging/macos/Info.plist" > "$APP/Contents/Info.plist"
cp "$ROOT/packaging/macos/Keel.icns" "$APP/Contents/Resources/"

# Ad-hoc signature (Apple Silicon refuses to run unsigned code).
# Swap "-" for a "Developer ID Application" identity once there is one.
codesign --force --deep --sign - "$APP"

# Disk image with an Applications shortcut for drag-to-install.
STAGE="$WORK/dmg"
mkdir -p "$STAGE"
cp -R "$APP" "$STAGE/"
ln -s /Applications "$STAGE/Applications"
hdiutil create -volname "Keel" -srcfolder "$STAGE" -ov -format UDZO "$DMG" >/dev/null

echo "Built $DMG"
