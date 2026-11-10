package setup

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Tosccony/keel/internal/web"
)

// ErrNoWebServer means neither a bundled Apache and PHP nor a development
// setup could be found.
var ErrNoWebServer = errors.New("no Apache and PHP found")

// WebRuntime returns the Apache and PHP to run: the ones bundled with Keel,
// or, in a development build on macOS, the Mac's own Apache with
// Homebrew's PHP.
func (o Options) WebRuntime() (web.Runtime, error) {
	if exeDir := exeDir(); exeDir != "" {
		if rt, ok := bundledWeb(exeDir); ok {
			return rt, nil
		}
	}
	return labWeb()
}

// PHPMyAdmin returns phpMyAdmin's folder: the one bundled with Keel, or in
// a development build, phpmyadmin/ in Keel's folder (scripts/fetch-phpmyadmin.sh
// puts it there). It's "" when there's none.
func (o Options) PHPMyAdmin() string {
	if dir := exeDir(); dir != "" {
		if pma := bundledDir(dir, "phpmyadmin"); pma != "" {
			return pma
		}
	}
	if pma := filepath.Join(o.home(), "phpmyadmin"); fileExists(filepath.Join(pma, "index.php")) {
		return pma
	}
	return ""
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// WebFolder is where Keel keeps Apache's and PHP's generated config, logs
// and temporary files.
func (o Options) WebFolder() string { return filepath.Join(o.home(), "web") }

// bundledWeb finds apache/ and php/ where each installer puts them (see
// findBundled).
func bundledWeb(exeDir string) (web.Runtime, bool) {
	apache, php := bundledDir(exeDir, "apache"), bundledDir(exeDir, "php")
	if apache == "" || php == "" {
		return web.Runtime{}, false
	}
	rt := web.Runtime{
		Httpd:     filepath.Join(apache, "bin", exe("httpd")),
		MimeTypes: filepath.Join(apache, "conf", "mime.types"),
		PHP:       filepath.Join(php, exe("php")),
	}
	if runtime.GOOS == "windows" {
		rt.PHPModule = filepath.Join(php, "php8apache2_4.dll")
		rt.ExtensionDir = filepath.Join(php, "ext")
	} else {
		rt.PHPFPM = filepath.Join(php, "php-fpm")
	}
	return rt, true
}

// labWeb is the development setup: macOS's own Apache, which has every
// module Keel needs, and Homebrew's PHP-FPM.
func labWeb() (web.Runtime, error) {
	if runtime.GOOS != "darwin" {
		return web.Runtime{}, ErrNoWebServer
	}
	rt := web.Runtime{
		Httpd:      "/usr/sbin/httpd",
		ModulesDir: "/usr/libexec/apache2",
		MimeTypes:  "/private/etc/apache2/mime.types",
	}
	for _, prefix := range []string{"/opt/homebrew", "/usr/local"} {
		if _, err := os.Stat(filepath.Join(prefix, "sbin", "php-fpm")); err == nil {
			rt.PHPFPM = filepath.Join(prefix, "sbin", "php-fpm")
			rt.PHP = filepath.Join(prefix, "bin", "php")
			break
		}
	}
	if _, err := os.Stat(rt.Httpd); err != nil || rt.PHPFPM == "" {
		return web.Runtime{}, ErrNoWebServer
	}
	return rt, nil
}

// Timezone is this computer's time zone as an IANA name, like
// "America/Halifax", for PHP's date.timezone. It's "" when it can't be
// told (Windows names its zones differently), and PHP then uses UTC.
func Timezone() string {
	if tz := os.Getenv("TZ"); strings.Contains(tz, "/") {
		return strings.TrimPrefix(tz, ":")
	}
	if runtime.GOOS == "windows" {
		return ""
	}
	link, err := filepath.EvalSymlinks("/etc/localtime")
	if err != nil {
		return ""
	}
	if _, name, ok := strings.Cut(filepath.ToSlash(link), "zoneinfo/"); ok {
		return name
	}
	return ""
}

func exeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return filepath.Dir(exe)
}

// bundledDir finds a folder that ships with Keel, in the places findBundled
// looks.
func bundledDir(exeDir, name string) string {
	for _, dir := range []string{
		filepath.Join(exeDir, name),
		filepath.Join(exeDir, "..", "Resources", name),
		filepath.Join(exeDir, "..", "lib", "keel", name),
	} {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return filepath.Clean(dir)
		}
	}
	return ""
}
