#!/usr/bin/env bash
# Checks an installed or portable Keel for Windows end to end. keel-lab.exe,
# copied beside keel.exe, runs the bundled MariaDB, Apache, PHP and
# phpMyAdmin from the same folders keel.exe would use (internal/setup), so
# this tests what students get, not a development setup.
#
#   first: start MariaDB and the web server, check PHP and its extensions,
#          MariaDB from PHP (mysqli and PDO, as root with no password),
#          phpMyAdmin and HTTPS, save a row, then stop everything.
#   again: after the drive got another letter: start again without setting
#          anything, and check the websites are served and the row is there.
#
# Usage: scripts/test-windows-package.sh first KEEL_DIR SITES_DIR
#        scripts/test-windows-package.sh again KEEL_DIR
# Run it in Git Bash, after copying keel-lab.exe into KEEL_DIR.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck source=../packaging/web/web.env
source "$ROOT/packaging/web/web.env"
WANT_PHP="${PHP_VERSION%.*}" # 8.4
MODE="${1:?usage: test-windows-package.sh first|again KEEL_DIR [SITES_DIR]}"
KEEL="$(cygpath -m "${2:?usage: test-windows-package.sh first|again KEEL_DIR [SITES_DIR]}")"
NOTE="kept after the move"
fail() { echo "test-windows-package: $*" >&2; exit 1; }
k() { "$KEEL/keel-lab.exe" "$@"; }

[ -f "$KEEL/keel.exe" ] || fail "no keel.exe in $KEEL"
[ -f "$KEEL/keel-lab.exe" ] || fail "copy keel-lab.exe into $KEEL first"

if [ "$MODE" = first ]; then
  SITES="$(cygpath -m "${3:?first needs SITES_DIR}")"
  k sites "$SITES"
  cat > "$SITES/check.php" <<'PHP'
<?php
echo "PHP ", PHP_VERSION, "\n";
$want = ["mysqli", "pdo_mysql", "mbstring", "openssl", "curl", "gd", "intl", "zip",
    "fileinfo", "sodium", "exif", "sqlite3", "pdo_sqlite", "xml", "dom", "simplexml",
    "session", "ctype", "iconv", "bcmath", "Zend OPcache"];
foreach ($want as $e) {
    if (!extension_loaded($e)) echo "MISSING ", $e, "\n";
}
PHP
  # What course code does: "localhost", root, no password. mysqli takes
  # the port from Keel's php.ini; PDO has no such setting, so it's passed.
  cat > "$SITES/db.php" <<'PHP'
<?php
$db = new mysqli("localhost", "root", "");
$db->query("CREATE DATABASE IF NOT EXISTS keel_check");
$db->select_db("keel_check");
$db->query("CREATE TABLE IF NOT EXISTS notes (id INT PRIMARY KEY, body VARCHAR(50))");
if (isset($_GET["save"])) {
    $q = $db->prepare("REPLACE INTO notes VALUES (1, ?)");
    $q->bind_param("s", $_GET["save"]);
    $q->execute();
}
$pdo = new PDO("mysql:host=localhost;port=" . (int)$_GET["port"] . ";dbname=keel_check", "root", "");
echo "mysqli ", $db->server_info, "\n";
echo "note=", $pdo->query("SELECT body FROM notes WHERE id = 1")->fetchColumn(), "\n";
PHP
fi

k start
db_port="$(k status | sed -n 's/^Running on port \([0-9]*\),.*/\1/p')"
[ -n "$db_port" ] || fail "MariaDB isn't running after start"
out="$(k web-start)" || fail "web-start failed: $out"
echo "$out"
port="$(echo "$out" | sed -n 's|.*http://localhost:\([0-9]*\)/.*|\1|p' | tail -1)"
https_port="$(echo "$out" | sed -n 's|.*https://localhost:\([0-9]*\)/.*|\1|p' | tail -1)"
[ -n "$port" ] && [ -n "$https_port" ] || fail "no ports in: $out"

# Pages are read whole, without -f, so a PHP error shows in the log.
page="$(curl -sS "http://127.0.0.1:$port/check.php")" || fail "check.php didn't load"
echo "$page"
grep -q "^PHP $WANT_PHP\." <<<"$page" || fail "not PHP $WANT_PHP"
if grep -q MISSING <<<"$page"; then fail "PHP is missing extensions"; fi

query="port=$db_port"
if [ "$MODE" = first ]; then query="$query&save=${NOTE// /+}"; fi
page="$(curl -sS "http://127.0.0.1:$port/db.php?$query")" || fail "db.php didn't load"
echo "$page"
grep -q "^mysqli .*MariaDB" <<<"$page" || fail "PHP couldn't reach MariaDB with mysqli"
grep -q "^note=$NOTE$" <<<"$page" || fail "PDO didn't read the saved row"

pma="$(curl -sS "http://127.0.0.1:$port/phpmyadmin/")" || fail "phpMyAdmin didn't load"
grep -q "phpMyAdmin" <<<"$pma" || fail "phpMyAdmin's page isn't phpMyAdmin"
secure="$(curl -sSk "https://127.0.0.1:$https_port/check.php")" || fail "HTTPS didn't answer"
grep -q "^PHP $WANT_PHP\." <<<"$secure" || fail "HTTPS didn't serve PHP"
echo "phpMyAdmin and HTTPS work"

k web-stop
k web-status | grep -q "not running" || fail "web server still running after web-stop"
out="$(k stop)"
echo "$out"
grep -q "^Stopped cleanly" <<<"$out" || fail "MariaDB didn't stop cleanly"
echo "Windows package OK ($MODE): $KEEL"
