#!/usr/bin/env bash
# Builds the MariaDB that Keel ships on macOS, from MariaDB's official
# source code (version and checksum in packaging/mariadb/mariadb.env).
# MariaDB publishes Windows and Linux builds (see fetch-mariadb.sh), but
# none for macOS.
#
# SSL, zlib, PCRE and fmt come from MariaDB's source tree instead of
# Homebrew, so the result runs on any Mac. Storage engines and plugins that
# need other libraries, or that a PHP class project doesn't use, are left
# out.
#
# Needs Xcode's command-line tools, cmake and bison 3 (brew install cmake
# bison). Result: dist/mariadb/macos-<arch>/mariadb
#
# Usage: scripts/build-mariadb.sh [arm64|x86_64]   (default: this Mac's)
# Set JOBS to limit parallel compile jobs.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck source=../packaging/mariadb/mariadb.env
source "$ROOT/packaging/mariadb/mariadb.env"

if [ "$(uname -s)" != Darwin ]; then
  echo "build-mariadb: this builds MariaDB for macOS; use fetch-mariadb.sh for Windows and Linux" >&2
  exit 2
fi
OS=macos
ARCH="${1:-$(uname -m)}"
JOBS="${JOBS:-$(sysctl -n hw.ncpu)}"
# Homebrew's bison is keg-only; macOS's own bison 2.3 is too old.
for b in /opt/homebrew/opt/bison/bin /usr/local/opt/bison/bin; do
  if [ -x "$b/bison" ]; then PATH="$b:$PATH"; fi
done

WORK="$ROOT/dist/mariadb-build/$OS-$ARCH"
OUT="$ROOT/dist/mariadb/$OS-$ARCH/mariadb"
TARBALL="$ROOT/dist/mariadb-build/mariadb-$MARIADB_VERSION.tar.gz"

mkdir -p "$WORK" "$(dirname "$TARBALL")"
if [ ! -f "$TARBALL" ]; then
  curl -fsSL -o "$TARBALL.part" \
    "https://archive.mariadb.org/mariadb-$MARIADB_VERSION/source/mariadb-$MARIADB_VERSION.tar.gz"
  mv "$TARBALL.part" "$TARBALL"
fi
echo "$MARIADB_SOURCE_SHA256  $TARBALL" | shasum -a 256 -c -

SRC="$WORK/src"
rm -rf "$SRC" "$WORK/build" "$WORK/install"
mkdir -p "$SRC"
tar -xzf "$TARBALL" -C "$SRC" --strip-components=1

CMAKE_ARGS=(
  -DCMAKE_BUILD_TYPE=Release
  -DCMAKE_INSTALL_PREFIX="$WORK/install"
  -DINSTALL_LAYOUT=STANDALONE
  -DINSTALL_MYSQLTESTDIR=
  -DINSTALL_SQLBENCHDIR=
  -DINSTALL_DOCDIR=
  -DINSTALL_DOCREADMEDIR=
  -DINSTALL_MANDIR=
  -DINSTALL_INFODIR=
  -DCOMPILATION_COMMENT="Keel"

  # Everything from MariaDB's own source tree, nothing from the system.
  -DWITH_SSL=bundled
  -DWITH_ZLIB=bundled
  -DWITH_PCRE=bundled
  -DWITH_LIBFMT=bundled

  -DWITH_UNIT_TESTS=OFF
  -DWITH_EMBEDDED_SERVER=OFF
  -DWITH_WSREP=OFF
  -DWITH_SYSTEMD=no
  -DWITH_NUMA=OFF
  -DWITH_MARIABACKUP=OFF
  -DWITH_JEMALLOC=no
  -DCONC_WITH_UNIT_TESTS=OFF

  # Storage engines and plugins that need extra system libraries, or that
  # a PHP class project doesn't use.
  -DPLUGIN_MROONGA=NO
  -DPLUGIN_ROCKSDB=NO
  -DPLUGIN_SPIDER=NO
  -DPLUGIN_CONNECT=NO
  -DPLUGIN_S3=NO
  -DPLUGIN_COLUMNSTORE=NO
  -DPLUGIN_OQGRAPH=NO
  -DPLUGIN_SPHINX=NO
  -DPLUGIN_AUTH_GSSAPI=NO
  -DPLUGIN_AUTH_PAM=NO
  -DPLUGIN_AUTH_PAM_V1=NO
  -DPLUGIN_HASHICORP_KEY_MANAGEMENT=NO
  -DPLUGIN_PROVIDER_BZIP2=NO
  -DPLUGIN_PROVIDER_LZ4=NO
  -DPLUGIN_PROVIDER_LZMA=NO
  -DPLUGIN_PROVIDER_LZO=NO
  -DPLUGIN_PROVIDER_SNAPPY=NO
)
CMAKE_ARGS+=(
  -DCMAKE_OSX_ARCHITECTURES="$ARCH"
  -DCMAKE_OSX_DEPLOYMENT_TARGET="${MACOSX_DEPLOYMENT_TARGET:-12.0}"
)

cmake -S "$SRC" -B "$WORK/build" "${CMAKE_ARGS[@]}"
cmake --build "$WORK/build" --parallel "$JOBS"
cmake --install "$WORK/build"

"$ROOT/scripts/trim-mariadb.sh" "$WORK/install" "$OUT"
echo "Built $OUT"
