#!/usr/bin/env bash
# Joins Apple Silicon and Intel builds (MariaDB, Apache, PHP) into one universal build,
# like Keel's own universal binary, and signs every program and plugin
# (ad hoc; Apple Silicon refuses to run unsigned code).
#
# Usage: scripts/merge-macos.sh ARM64_DIR X86_64_DIR OUT_DIR
set -euo pipefail

ARM="$1"
X86="$2"
OUT="$3"
rm -rf "$OUT"
mkdir -p "$OUT"
(cd "$ARM" && find . -type d) | while read -r d; do mkdir -p "$OUT/$d"; done
(cd "$ARM" && find . -type f) | while read -r f; do
  if file "$ARM/$f" | grep -q "Mach-O"; then
    lipo -create "$ARM/$f" "$X86/$f" -output "$OUT/$f"
    codesign --force --sign - "$OUT/$f"
  else
    cp -p "$ARM/$f" "$OUT/$f"
  fi
done
# Show what was joined.
find "$OUT" -type f | while read -r f; do
  if file "$f" | grep -q "Mach-O"; then lipo -info "$f"; fi
done
