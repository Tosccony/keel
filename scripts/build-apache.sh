#!/usr/bin/env bash
# Builds the Apache that Keel ships on macOS and Linux, from source
# (versions and checksums in packaging/web/web.env). Neither system has an
# official Apache build Keel can carry.
#
# OpenSSL, PCRE2, expat, APR and APR-util are built as static libraries,
# and the modules XAMPP enables are compiled into httpd itself, so the
# result is one program that needs only the system's basic libraries and
# runs from wherever Keel is installed.
#
# Result: dist/apache/<os>-<arch>/apache, holding bin/httpd, conf/mime.types
# and Apache's licence.
#
# Usage: scripts/build-apache.sh [arch]   (default: this machine's)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck source=../packaging/web/web.env
source "$ROOT/packaging/web/web.env"

case "$(uname -s)" in
  Darwin) OS=macos ;;
  Linux) OS=linux ;;
  *) echo "build-apache: use fetch-web-windows.sh on Windows" >&2; exit 2 ;;
esac
ARCH="${1:-$(uname -m)}"
[ "$ARCH" = aarch64 ] && ARCH=arm64
JOBS="${JOBS:-$(getconf _NPROCESSORS_ONLN 2>/dev/null || sysctl -n hw.ncpu)}"

SRC="$ROOT/dist/web-build/src"
WORK="$ROOT/dist/web-build/apache-$OS-$ARCH"
DEPS="$WORK/deps"
OUT="$ROOT/dist/apache/$OS-$ARCH/apache"
mkdir -p "$SRC"

sha256() {
  if command -v sha256sum >/dev/null; then sha256sum "$1" | cut -d' ' -f1; else shasum -a 256 "$1" | cut -d' ' -f1; fi
}
# fetch NAME URL SHA256: download into src/ once, and check it every time.
fetch() {
  local file="$SRC/$1"
  if [ ! -f "$file" ]; then
    curl -fsSL --retry 3 -o "$file.part" "$2"
    mv "$file.part" "$file"
  fi
  if [ "$(sha256 "$file")" != "$3" ]; then
    echo "build-apache: $1 doesn't match its checksum" >&2
    exit 1
  fi
}
fetch "httpd-$HTTPD_VERSION.tar.bz2" "https://archive.apache.org/dist/httpd/httpd-$HTTPD_VERSION.tar.bz2" "$HTTPD_SHA256"
fetch "apr-$APR_VERSION.tar.bz2" "https://archive.apache.org/dist/apr/apr-$APR_VERSION.tar.bz2" "$APR_SHA256"
fetch "apr-util-$APRUTIL_VERSION.tar.bz2" "https://archive.apache.org/dist/apr/apr-util-$APRUTIL_VERSION.tar.bz2" "$APRUTIL_SHA256"
fetch "pcre2-$PCRE2_VERSION.tar.bz2" "https://github.com/PCRE2Project/pcre2/releases/download/pcre2-$PCRE2_VERSION/pcre2-$PCRE2_VERSION.tar.bz2" "$PCRE2_SHA256"
fetch "expat-$EXPAT_VERSION.tar.xz" "https://github.com/libexpat/libexpat/releases/download/$EXPAT_TAG/expat-$EXPAT_VERSION.tar.xz" "$EXPAT_SHA256"
fetch "openssl-$OPENSSL_VERSION.tar.gz" "https://github.com/openssl/openssl/releases/download/openssl-$OPENSSL_VERSION/openssl-$OPENSSL_VERSION.tar.gz" "$OPENSSL_SHA256"

rm -rf "$WORK"
mkdir -p "$WORK" "$DEPS"
unpack() { tar -xf "$SRC/$1" -C "$WORK"; }

export CFLAGS="-O2 -fPIC"
case "$OS-$ARCH" in
  macos-*)
    export MACOSX_DEPLOYMENT_TARGET="${MACOSX_DEPLOYMENT_TARGET:-12.0}"
    # Calls to functions newer than the deployment target are errors, so
    # configure scripts see them as missing instead of using them (a newer
    # SDK declares dup3, for example, which macOS 12 doesn't have).
    export CFLAGS="$CFLAGS -arch $ARCH -Werror=unguarded-availability-new"
    export LDFLAGS="-arch $ARCH"
    ZLIB=(--with-z="$(xcrun --show-sdk-path)/usr")
    SSL_TARGET="darwin64-$ARCH-cc"
    [ "$ARCH" = arm64 ] && SSL_TARGET=darwin64-arm64-cc
    ;;
  linux-x86_64) SSL_TARGET=linux-x86_64 ;;
  linux-arm64) SSL_TARGET=linux-aarch64 ;;
esac
if [ -z "${ZLIB+set}" ]; then ZLIB=(); fi
HOST_FLAGS=()
if [ "$OS" = macos ] && [ "$ARCH" != "$(uname -m)" ]; then
  HOST_FLAGS=(--host="$( [ "$ARCH" = arm64 ] && echo aarch64 || echo x86_64 )-apple-darwin")
fi

echo "== OpenSSL $OPENSSL_VERSION"
unpack "openssl-$OPENSSL_VERSION.tar.gz"
(cd "$WORK/openssl-$OPENSSL_VERSION" &&
  ./Configure "$SSL_TARGET" no-shared no-tests no-docs --prefix="$DEPS" --libdir=lib >/dev/null &&
  make -j"$JOBS" >/dev/null && make install_sw >/dev/null)

echo "== PCRE2 $PCRE2_VERSION"
unpack "pcre2-$PCRE2_VERSION.tar.bz2"
(cd "$WORK/pcre2-$PCRE2_VERSION" &&
  ./configure "${HOST_FLAGS[@]}" --prefix="$DEPS" --disable-shared --enable-static >/dev/null &&
  make -j"$JOBS" >/dev/null && make install >/dev/null)

echo "== expat $EXPAT_VERSION"
unpack "expat-$EXPAT_VERSION.tar.xz"
(cd "$WORK/expat-$EXPAT_VERSION" &&
  ./configure "${HOST_FLAGS[@]}" --prefix="$DEPS" --disable-shared --enable-static \
    --without-docbook --without-examples --without-tests >/dev/null &&
  make -j"$JOBS" >/dev/null && make install >/dev/null)

echo "== APR $APR_VERSION"
unpack "apr-$APR_VERSION.tar.bz2"
(cd "$WORK/apr-$APR_VERSION" &&
  ./configure "${HOST_FLAGS[@]}" --prefix="$DEPS" --disable-shared --enable-static >/dev/null &&
  make -j"$JOBS" >/dev/null && make install >/dev/null)

echo "== APR-util $APRUTIL_VERSION"
unpack "apr-util-$APRUTIL_VERSION.tar.bz2"
(cd "$WORK/apr-util-$APRUTIL_VERSION" &&
  ./configure "${HOST_FLAGS[@]}" --prefix="$DEPS" --with-apr="$DEPS" --with-expat="$DEPS" \
    --without-crypto --without-ldap --without-pgsql \
    --without-mysql --without-sqlite3 --without-sqlite2 --without-oracle --without-odbc \
    --without-berkeley-db --without-gdbm --without-ndbm >/dev/null &&
  make -j"$JOBS" >/dev/null && make install >/dev/null)

echo "== Apache $HTTPD_VERSION"
unpack "httpd-$HTTPD_VERSION.tar.bz2"
# The modules XAMPP enables that a PHP course uses (see internal/web), all
# compiled in. "proxy" would pull in every proxy module; only FastCGI (for
# PHP-FPM), HTTP and WebSocket proxying stay.
MODULES="authz_core authz_host authz_user authn_core authn_file auth_basic access_compat
  unixd dir mime log_config alias rewrite headers setenvif env filter deflate expires
  proxy proxy_fcgi autoindex include negotiation version cgid ssl socache_shmcb"
(cd "$WORK/httpd-$HTTPD_VERSION" &&
  ./configure "${HOST_FLAGS[@]}" --prefix="$WORK/install" \
    --with-apr="$DEPS" --with-apr-util="$DEPS" --with-pcre="$DEPS/bin/pcre2-config" \
    --with-ssl="$DEPS" "${ZLIB[@]}" --with-mpm=event --enable-mods-static="$(echo $MODULES)" \
    --enable-modules=none \
    --disable-proxy-balancer --disable-proxy-ajp --disable-proxy-express \
    --disable-proxy-ftp --disable-proxy-scgi --disable-proxy-uwsgi \
    --disable-proxy-fdpass --disable-proxy-connect --disable-lbmethod-byrequests \
    --disable-lbmethod-bytraffic --disable-lbmethod-bybusyness \
    --disable-lbmethod-heartbeat >/dev/null &&
  make -j"$JOBS" >/dev/null && make install >/dev/null)

rm -rf "$OUT"
mkdir -p "$OUT/bin" "$OUT/conf"
cp "$WORK/install/bin/httpd" "$OUT/bin/"
cp "$WORK/install/conf/mime.types" "$OUT/conf/"
cp "$WORK/httpd-$HTTPD_VERSION/LICENSE" "$WORK/httpd-$HTTPD_VERSION/NOTICE" "$OUT/"
"$OUT/bin/httpd" -v
"$OUT/bin/httpd" -l | tr '\n' ' '
echo
echo "Built $OUT"
