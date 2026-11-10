#!/usr/bin/env bash
# Checks Keel's Apache and PHP builds with Keel's own code (keel-lab): start
# the web server, check PHP and every extension Keel promises, a .htaccess
# rewrite and, if given, phpMyAdmin, then stop.
#
# Usage: scripts/test-web-runtime.sh APACHE_DIR PHP_DIR [PHPMYADMIN_DIR]
# Set EXPECT_PHP to test a PHP other than the one Keel ships (like 8.5).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck source=../packaging/web/web.env
source "$ROOT/packaging/web/web.env"
WANT_PHP="${EXPECT_PHP:-${PHP_VERSION%.*}}" # 8.4; EXPECT_PHP overrides for local runs
APACHE="$(cd "$1" && pwd)"
PHP="$(cd "$2" && pwd)"
PMA=""
if [ -n "${3:-}" ]; then PMA="$(cd "$3" && pwd)"; fi
DIR="$(mktemp -d)"
EXE=""
if [ -f "$APACHE/bin/httpd.exe" ]; then EXE=.exe; fi
if command -v cygpath >/dev/null; then # Git Bash on Windows: give programs C:/ paths
  APACHE="$(cygpath -m "$APACHE")"
  PHP="$(cygpath -m "$PHP")"
  if [ -n "$PMA" ]; then PMA="$(cygpath -m "$PMA")"; fi
  DIR="$(cygpath -m "$DIR")"
fi
fail() { echo "test-web-runtime: $*" >&2; exit 1; }

(cd "$ROOT" && go build -o "$DIR/keel-lab$EXE" ./cmd/keel-lab)
export KEEL_APACHE="$APACHE" KEEL_PHP="$PHP"
if [ -n "$PMA" ]; then export KEEL_PHPMYADMIN="$PMA"; fi
k() { "$DIR/keel-lab$EXE" -lab "$DIR/lab" "$@"; }

k sites "$DIR/htdocs"
cat > "$DIR/htdocs/check.php" <<'PHP'
<?php
echo "PHP ", PHP_VERSION, "\n";
$want = ["mysqli", "pdo_mysql", "mbstring", "openssl", "curl", "gd", "intl", "zip",
    "fileinfo", "sodium", "exif", "sqlite3", "pdo_sqlite", "xml", "dom", "simplexml",
    "session", "ctype", "iconv", "bcmath", "Zend OPcache"];
foreach ($want as $e) {
    if (!extension_loaded($e)) echo "MISSING ", $e, "\n";
}
echo "upload=", ini_get("upload_max_filesize"), "\n";
echo "https=", in_array("https", stream_get_wrappers()) ? "yes" : "no", "\n";
PHP
mkdir -p "$DIR/htdocs/shop"
printf 'RewriteEngine On\nRewriteRule ^p/([0-9]+)$ show.php?id=$1 [L]\n' > "$DIR/htdocs/shop/.htaccess"
echo '<?php echo "product ", (int)$_GET["id"];' > "$DIR/htdocs/shop/show.php"

# Perl scripts, the way tutorials write them. On Windows this downloads
# Strawberry Perl and relies on the registry association Keel sets up.
k perl on
printf '#!/usr/bin/perl\nprint "Content-Type: text/plain\\n\\n";\nprint "hello from perl $]\\n";\n' > "$DIR/htdocs/hello.pl"
chmod +x "$DIR/htdocs/hello.pl"

out="$(k web-start)" || fail "web-start failed: $out"
echo "$out"
port="$(echo "$out" | sed -n 's|.*http://localhost:\([0-9]*\)/.*|\1|p' | tail -1)"
[ -n "$port" ] || fail "no port in: $out"

page="$(curl -fsS "http://127.0.0.1:$port/check.php")" || fail "check.php didn't load"
echo "$page"
echo "$page" | grep -q "^PHP $WANT_PHP\." || fail "not PHP $WANT_PHP"
if echo "$page" | grep -q MISSING; then fail "PHP is missing extensions"; fi
echo "$page" | grep -q "upload=64M" || fail "Keel's php.ini wasn't used"
[ "$(curl -fsS "http://127.0.0.1:$port/shop/p/7")" = "product 7" ] || fail ".htaccess rewrite didn't work"
if [ -n "$PMA" ]; then
  # Read the whole page first: grep -q stops reading at the first match,
  # and with pipefail curl's "Failed writing body" would fail the check.
  pma="$(curl -fsS "http://127.0.0.1:$port/phpmyadmin/")" || fail "phpMyAdmin didn't load"
  grep -q "phpMyAdmin" <<<"$pma" || fail "phpMyAdmin's page isn't phpMyAdmin"
  echo "phpMyAdmin loads"
fi
perl_out="$(curl -fsS "http://127.0.0.1:$port/hello.pl")" || fail "hello.pl didn't run"
echo "$perl_out"
echo "$perl_out" | grep -q "^hello from perl 5\." || fail "Perl CGI didn't work"
k web-stop
k web-status | grep -q "not running" || fail "still running after web-stop"
echo "Web runtime OK"
