package setup

import (
	"context"
	"errors"
	"fmt"
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
//
// KEEL_APACHE and KEEL_PHP, if both set, name the folders to use instead:
// the CI tests point them at freshly trimmed builds.
func (o Options) WebRuntime() (web.Runtime, error) {
	if apache, php := os.Getenv("KEEL_APACHE"), os.Getenv("KEEL_PHP"); apache != "" && php != "" {
		return webFrom(apache, php), nil
	}
	if exeDir := exeDir(); exeDir != "" {
		if apache, php := bundledDir(exeDir, "apache"), bundledDir(exeDir, "php"); apache != "" && php != "" {
			return webFrom(apache, php), nil
		}
	}
	return labWeb()
}

// Web returns the web server, with Keel's folder and saved ports.
func (o Options) Web() (*web.Server, error) {
	rt, err := o.WebRuntime()
	if err != nil {
		return nil, err
	}
	st := o.Settings()
	return &web.Server{
		Runtime: rt,
		Folder:  o.WebFolder(),
		Port:    st.Port(PartWeb, DefaultPorts[PartWeb]),
		FPMPort: st.Port(PartPHP, DefaultPorts[PartPHP]),
	}, nil
}

// PrepareWeb gets the web server ready to start: it moves its ports off
// other programs, and fills in the websites folder, MariaDB's port and
// socket, the mail catcher's addresses, the sendmail command, the time
// zone and phpMyAdmin. An empty websites folder gets the welcome page. The
// note is for the user; it's empty when nothing needed saying.
func (o Options) PrepareWeb(ctx context.Context, s *web.Server, sendmail string) (string, error) {
	st := o.Settings()
	sites := st.Sites()
	if sites == "" {
		return "", errors.New("choose a websites folder first")
	}
	var notes []string
	port, note, err := o.MovePort(ctx, PartWeb, "the web server", s.Port)
	if err != nil {
		return "", err
	}
	notes = append(notes, note)
	fpm := s.FPMPort
	if s.PHPFPM != "" {
		if fpm, note, err = o.MovePort(ctx, PartPHP, "PHP", fpm); err != nil {
			return "", err
		}
		notes = append(notes, note)
	}
	s.Sites, s.Port, s.FPMPort = sites, port, fpm
	s.MariaDBPort = o.MariaDBPort()
	s.MariaDBSocket = o.Server().Socket
	s.MailPort = st.Port(PartMail, DefaultPorts[PartMail])
	s.MailInbox = fmt.Sprintf("http://localhost:%d/", st.Port(PartMailWeb, DefaultPorts[PartMailWeb]))
	s.Sendmail = sendmail
	s.Timezone = Timezone()
	s.PHPMyAdmin = o.PHPMyAdmin()
	if wrote, err := web.WriteWelcome(sites); err == nil && wrote {
		notes = append(notes, "The websites folder was empty, so Keel put a welcome page in it (index.php).")
	}
	return strings.TrimSpace(strings.Join(notes, " ")), nil
}

// PHPMyAdmin returns phpMyAdmin's folder: the one bundled with Keel, or in
// a development build, phpmyadmin/ in Keel's folder (scripts/fetch-phpmyadmin.sh
// puts it there). It's "" when there's none.
func (o Options) PHPMyAdmin() string {
	if dir := os.Getenv("KEEL_PHPMYADMIN"); dir != "" {
		return dir
	}
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

// webFrom describes Keel's own Apache and PHP builds, as each installer
// lays them out. On macOS and Linux Apache's modules are compiled in; on
// Windows they're files in modules/, and PHP is loaded into Apache.
func webFrom(apache, php string) web.Runtime {
	rt := web.Runtime{
		Httpd:     filepath.Join(apache, "bin", exe("httpd")),
		MimeTypes: filepath.Join(apache, "conf", "mime.types"),
		PHP:       filepath.Join(php, exe("php")),
	}
	if runtime.GOOS == "windows" {
		rt.ModulesDir = filepath.Join(apache, "modules")
		rt.PHPModule = filepath.Join(php, "php8apache2_4.dll")
		rt.ExtensionDir = filepath.Join(php, "ext")
	} else {
		rt.PHPFPM = filepath.Join(php, "php-fpm")
	}
	return rt
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
