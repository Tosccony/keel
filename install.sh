#!/bin/sh
# Installs the latest Keel release on macOS or Linux.
#   curl -fsSL https://raw.githubusercontent.com/Tosccony/keel/main/install.sh | sh
set -eu

BASE="https://github.com/Tosccony/keel/releases/latest/download"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fetch() { curl -fL --progress-bar -o "$TMP/$1" "$BASE/$1"; }

case "$(uname -s)" in
  Darwin)
    echo "Downloading Keel for macOS..."
    fetch Keel-macOS.dmg
    MNT="$(hdiutil attach -nobrowse -readonly "$TMP/Keel-macOS.dmg" | awk -F'\t' '/\/Volumes\//{print $NF}')"
    rm -rf /Applications/Keel.app
    cp -R "$MNT/Keel.app" /Applications/
    hdiutil detach "$MNT" -quiet
    echo "Installed /Applications/Keel.app"
    ;;
  Linux)
    if [ "$(uname -m)" != "x86_64" ]; then
      echo "Keel only has x86_64 Linux builds so far." >&2
      exit 1
    fi
    if command -v apt-get >/dev/null 2>&1; then
      echo "Downloading Keel (.deb)..."
      fetch keel_amd64.deb
      sudo apt-get install -y "$TMP/keel_amd64.deb"
    elif command -v dnf >/dev/null 2>&1; then
      echo "Downloading Keel (.rpm)..."
      fetch keel_x86_64.rpm
      sudo dnf install -y "$TMP/keel_x86_64.rpm"
    elif command -v zypper >/dev/null 2>&1; then
      echo "Downloading Keel (.rpm)..."
      fetch keel_x86_64.rpm
      sudo zypper --non-interactive install --allow-unsigned-rpm "$TMP/keel_x86_64.rpm"
    else
      echo "Downloading Keel (.tar.gz)..."
      fetch keel_linux_x86_64.tar.gz
      mkdir -p "$HOME/.local/bin" "$HOME/.local/share/applications"
      tar -xzf "$TMP/keel_linux_x86_64.tar.gz" -C "$TMP"
      install -m 0755 "$TMP/keel" "$HOME/.local/bin/keel"
      install -m 0644 "$TMP/keel.desktop" "$HOME/.local/share/applications/keel.desktop"
      echo "Installed ~/.local/bin/keel (make sure ~/.local/bin is on your PATH)"
    fi
    echo "Keel is installed. Open it from your app menu or run: keel"
    ;;
  *)
    echo "This script supports macOS and Linux. On Windows, use install.ps1." >&2
    exit 1
    ;;
esac
