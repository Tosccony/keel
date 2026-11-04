#!/usr/bin/env bash
# Copies the parts of a MariaDB build that Keel uses from SRC to DST.
#
# SRC is an unpacked official build (Windows .zip, Linux .tar.gz) or the
# install folder of scripts/build-mariadb.sh. They share a layout: bin/,
# scripts/ (Linux and macOS), share/ and lib/plugin/.
#
# Everything is an allow-list, so a new MariaDB release can't slip test
# programs or heavy plugins into Keel. The result:
#   bin/         the server, setup, repair tools and the command-line client
#   share/       error messages, character sets, and the SQL a new data
#                folder is set up with
#   lib/plugin/  login plugins and a few small, useful ones
#   COPYING ...  MariaDB's licence (GPL v2), README and third-party notices
#
# Linux builds carry debugging symbols (mariadbd alone is 392 MB); they're
# stripped. Set STRIP to the strip program to use (default: strip).
#
# Usage: scripts/trim-mariadb.sh SRC DST
set -euo pipefail

SRC="$1"
DST="$2"
if [ -f "$SRC/bin/mariadbd.exe" ]; then
  EXE=.exe PLUGIN=.dll
else
  EXE="" PLUGIN=.so
fi

rm -rf "$DST"
mkdir -p "$DST/bin" "$DST/share" "$DST/lib/plugin"

# The server; the repair tools Keel runs (aria_chk, myisamchk); the client
# programs students use; and mariadb-upgrade, for when Keel moves to a
# newer MariaDB.
PROGRAMS=(mariadbd aria_chk myisamchk mariadb mariadb-dump mariadb-admin
  mariadb-check mariadb-import mariadb-upgrade)
if [ -n "$EXE" ]; then
  # On Windows, setup is its own program, and it runs the server under its
  # old name, mysqld.exe (a 13 KB copy of mariadbd.exe; the server itself
  # is in server.dll).
  PROGRAMS+=(mariadb-install-db mysqld)
else
  # On Linux and macOS, setup is a script in scripts/ that runs these two.
  PROGRAMS+=(my_print_defaults resolveip)
  cp "$SRC/scripts/mariadb-install-db" "$DST/bin/"
fi
for p in "${PROGRAMS[@]}"; do
  cp "$SRC/bin/$p$EXE" "$DST/bin/"
done

if [ -n "$EXE" ]; then
  # The server's DLL, and the Visual C++ runtime MariaDB ships beside it.
  for f in server.dll msvcp140.dll msvcp140_1.dll msvcp140_2.dll \
    vcruntime140.dll vcruntime140_1.dll concrt140.dll zlib1.dll; do
    if [ -f "$SRC/bin/$f" ]; then cp "$SRC/bin/$f" "$DST/bin/"; fi
  done
fi

# Plugins that need nothing beyond the system's basic libraries. Missing
# ones are fine: not every system has every plugin.
for p in auth_ed25519 client_ed25519 caching_sha2_password sha256_password \
  mysql_clear_password dialog parsec auth_named_pipe \
  authentication_windows_client file_key_management ha_archive \
  ha_blackhole ha_federated ha_federatedx locales metadata_lock_info \
  password_reuse_check simple_password_check query_cache_info \
  query_response_time server_audit sql_errlog type_mysql_json \
  type_mysql_timestamp disks; do
  if [ -f "$SRC/lib/plugin/$p$PLUGIN" ]; then cp "$SRC/lib/plugin/$p$PLUGIN" "$DST/lib/plugin/"; fi
done

# share/: every language's error messages, the character sets, and the
# setup SQL. Not CONNECT's Java files or Mroonga's data.
for f in "$SRC"/share/*; do
  name="$(basename "$f")"
  if [ -f "$f/errmsg.sys" ] || [ "$name" = charsets ]; then
    cp -R "$f" "$DST/share/"
  elif [[ "$name" == *.sql ]]; then
    cp "$f" "$DST/share/"
  fi
done
for f in COPYING README.md CREDITS THIRDPARTY; do
  if [ -f "$SRC/$f" ]; then cp "$SRC/$f" "$DST/"; fi
done

if [ -z "$EXE" ] && head -c 4 "$DST/bin/mariadbd" | grep -q "ELF"; then
  for f in "$DST"/bin/* "$DST"/lib/plugin/*; do
    if head -c 4 "$f" | grep -q "ELF"; then "${STRIP:-strip}" --strip-debug "$f"; fi
  done
fi

# Fail loudly if anything Keel needs is missing.
for need in "bin/mariadbd$EXE" "bin/mariadb-install-db$EXE" "bin/aria_chk$EXE" \
  share/english/errmsg.sys share/charsets share/mariadb_system_tables.sql; do
  if [ ! -e "$DST/$need" ]; then
    if [ "$need" = "bin/mariadb-install-db$EXE" ] && [ -f "$DST/bin/mariadb-install-db" ]; then continue; fi
    echo "trim-mariadb: $need is missing from $SRC" >&2
    exit 1
  fi
done
du -sh "$DST"
