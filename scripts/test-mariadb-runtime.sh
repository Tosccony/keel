#!/usr/bin/env bash
# Checks a trimmed MariaDB (dist/mariadb/<os>-<arch>/mariadb) with Keel's
# own code: set up a data folder, start, check every table, connect over
# TLS with a password (MariaDB's own client insists on TLS once there's a
# password), stop cleanly, back up, repair, start and stop again.
#
# Usage: scripts/test-mariadb-runtime.sh dist/mariadb/linux-x86_64/mariadb
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
RUNTIME="$(cd "$1" && pwd)"
BIN="$RUNTIME/bin"
DIR="$(mktemp -d)"
EXE=""
if [ -f "$BIN/mariadbd.exe" ]; then EXE=.exe; fi
if command -v cygpath >/dev/null; then # Git Bash on Windows: give programs C:/ paths
  BIN="$(cygpath -m "$BIN")"
  DIR="$(cygpath -m "$DIR")"
fi
PORT=3399

(cd "$ROOT" && go build -o "$DIR/keel-lab$EXE" ./cmd/keel-lab)
k() { "$DIR/keel-lab$EXE" -bin "$BIN" -lab "$DIR/lab" -port "$PORT" "$@"; }
sql() { "$BIN/mariadb$EXE" -h127.0.0.1 -P"$PORT" -uroot "$@"; }

k init
k start
k check

echo "--- TLS with a password"
sql -e "ALTER USER IF EXISTS root@localhost IDENTIFIED BY 'pw';
        ALTER USER IF EXISTS root@'127.0.0.1' IDENTIFIED BY 'pw';
        ALTER USER IF EXISTS root@'::1' IDENTIFIED BY 'pw'"
cipher="$(sql -ppw -N -e "SHOW STATUS LIKE 'Ssl_cipher'")"
echo "$cipher"
if ! echo "$cipher" | grep -q '[A-Z]'; then
  echo "test-mariadb-runtime: no TLS cipher; a password login can't work" >&2
  exit 1
fi
sql -ppw -e "ALTER USER IF EXISTS root@localhost IDENTIFIED BY '';
             ALTER USER IF EXISTS root@'127.0.0.1' IDENTIFIED BY '';
             ALTER USER IF EXISTS root@'::1' IDENTIFIED BY ''"

k stop
k backup
k repair
k start
k status
k stop
k backups
echo "MariaDB runtime OK: $RUNTIME"
