package web

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// labRuntime is this Mac's own Apache and Homebrew's PHP, the development
// setup. Tests skip without them.
func labRuntime(t *testing.T) Runtime {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("the development web server is set up for macOS")
	}
	rt := Runtime{
		Httpd:      "/usr/sbin/httpd",
		ModulesDir: "/usr/libexec/apache2",
		MimeTypes:  "/private/etc/apache2/mime.types",
	}
	for _, p := range []string{"/opt/homebrew/sbin/php-fpm", "/usr/local/sbin/php-fpm"} {
		if _, err := os.Stat(p); err == nil {
			rt.PHPFPM = p
			rt.PHP = filepath.Join(filepath.Dir(filepath.Dir(p)), "bin", "php")
		}
	}
	if _, err := os.Stat(rt.Httpd); err != nil || rt.PHPFPM == "" {
		t.Skip("needs /usr/sbin/httpd and Homebrew's php-fpm")
	}
	return rt
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	dir, err := os.MkdirTemp("", "keel-web-") // short: Apache's runtime files have length limits
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	s := &Server{
		Runtime:     labRuntime(t),
		Folder:      filepath.Join(dir, "web"),
		Sites:       filepath.Join(dir, "htdocs"),
		Port:        freePort(t),
		FPMPort:     freePort(t),
		MariaDBPort: 3307,
		MailPort:    1025,
		MailInbox:   "http://localhost:8025/",
		Sendmail:    "/bin/cat",
		Timezone:    "America/Halifax",
	}
	os.MkdirAll(s.Sites, 0o755)
	return s
}

func get(t *testing.T, s *Server, path string) (int, string) {
	t.Helper()
	resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(s.Port) + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestServesPHP(t *testing.T) {
	s := newTestServer(t)
	os.WriteFile(filepath.Join(s.Sites, "index.php"), []byte(`<?php
echo "PHP ", PHP_VERSION, "\n";
echo "tz=", date_default_timezone_get(), "\n";
echo "upload=", ini_get("upload_max_filesize"), "\n";
echo "inbox=", $_SERVER["KEEL_MAIL_INBOX"] ?? "", "\n";
echo "mysqli=", extension_loaded("mysqli") ? "yes" : "no", "\n";
`), 0o644)
	os.MkdirAll(filepath.Join(s.Sites, "shop"), 0o755)
	os.WriteFile(filepath.Join(s.Sites, "shop", ".htaccess"), []byte("RewriteEngine On\nRewriteRule ^product/([0-9]+)$ show.php?id=$1 [L]\n"), 0o644)
	os.WriteFile(filepath.Join(s.Sites, "shop", "show.php"), []byte(`<?php echo "product ", (int)$_GET["id"];`), 0o644)

	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Stop(context.Background()) })
	if !s.Running() {
		t.Fatal("not running after Start")
	}

	code, body := get(t, s, "/")
	for _, want := range []string{"PHP 8.", "tz=America/Halifax", "upload=64M", "inbox=http://localhost:8025/", "mysqli=yes"} {
		if code != 200 || !strings.Contains(body, want) {
			t.Errorf("GET / = %d, missing %q:\n%s", code, want, body)
		}
	}
	if code, body := get(t, s, "/shop/product/42"); code != 200 || body != "product 42" {
		t.Errorf(".htaccess rewrite: %d %q", code, body)
	}
	if code, _ := get(t, s, "/shop/.htaccess"); code != 403 {
		t.Errorf(".htaccess should be hidden, got %d", code)
	}
	if apache, php := s.Versions(); !strings.HasPrefix(apache, "2.4") || !strings.HasPrefix(php, "8.") {
		t.Errorf("versions: Apache %q, PHP %q", apache, php)
	}

	// A new Server for the same folder finds the running one, as after
	// Keel restarts, and stops both Apache and PHP.
	again := &Server{Runtime: s.Runtime, Folder: s.Folder, Sites: s.Sites, Port: s.Port, FPMPort: s.FPMPort}
	if !again.Running() {
		t.Fatal("a fresh Server doesn't see the running Apache")
	}
	if err := again.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	for _, port := range []int{s.Port, s.FPMPort} {
		if c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), time.Second); err == nil {
			c.Close()
			t.Errorf("port %d still answers after Stop", port)
		}
	}
}

// The user's own settings files survive restarts, and a mistake in
// custom.conf is reported before Apache starts.
func TestCustomSettings(t *testing.T) {
	s := newTestServer(t)
	os.WriteFile(filepath.Join(s.Sites, "i.php"), []byte(`<?php echo ini_get("memory_limit");`), 0o644)
	if err := s.writeConfig(); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(s.CustomIni(), []byte("memory_limit = 512M\n"), 0o644)
	os.WriteFile(s.CustomConf(), []byte("Alias /extra \""+filepath.ToSlash(s.Sites)+"\"\n"), 0o644)
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, body := get(t, s, "/i.php"); body != "512M" {
		t.Errorf("custom.ini ignored: memory_limit = %q", body)
	}
	if code, _ := get(t, s, "/extra/i.php"); code != 200 {
		t.Errorf("custom.conf ignored: /extra gave %d", code)
	}
	s.Stop(context.Background())
	if b, _ := os.ReadFile(s.CustomIni()); string(b) != "memory_limit = 512M\n" {
		t.Errorf("Keel changed the user's custom.ini: %q", b)
	}

	os.WriteFile(s.CustomConf(), []byte("ThisIsNotADirective on\n"), 0o644)
	err := s.Start(context.Background())
	if err == nil {
		s.Stop(context.Background())
		t.Fatal("Start with a broken custom.conf succeeded")
	}
	if !strings.Contains(err.Error(), "custom.conf") || !strings.Contains(err.Error(), "ThisIsNotADirective") {
		t.Errorf("the error should point at the mistake: %v", err)
	}
	if s.Running() {
		t.Error("Apache is running after a failed config check")
	}
}

func TestStartFailsCleanly(t *testing.T) {
	s := newTestServer(t)
	s.Sites = filepath.Join(t.TempDir(), "missing")
	if err := s.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "websites folder") {
		t.Errorf("missing websites folder: %v", err)
	}

	s = newTestServer(t)
	busy, _ := net.Listen("tcp", "127.0.0.1:0")
	defer busy.Close()
	s.Port = busy.Addr().(*net.TCPAddr).Port
	s.ReadyTimeout = 5 * time.Second
	err := s.Start(context.Background())
	if err == nil {
		s.Stop(context.Background())
		t.Fatal("Start on a taken port succeeded")
	}
	if s.fpm.PID() != 0 {
		t.Error("PHP-FPM was left running after Apache failed")
	}
}

func TestGeneratedConfigPassesApachesCheck(t *testing.T) {
	s := newTestServer(t)
	s.PHPMyAdmin = s.Sites
	if err := s.writeConfig(); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(s.Httpd, "-t", "-f", filepath.Join(s.conf(), "httpd.conf")).CombinedOutput()
	if err != nil {
		t.Fatalf("httpd -t: %v\n%s", err, out)
	}
	b, _ := os.ReadFile(filepath.Join(s.conf(), "php.ini"))
	for _, want := range []string{`sendmail_path = "/bin/cat"`, "mysqli.default_port = 3307", `date.timezone = "America/Halifax"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("php.ini lacks %q", want)
		}
	}
}

func TestPHPMyAdminConfig(t *testing.T) {
	s := &Server{Folder: filepath.Join(t.TempDir(), "O'Brien's web"), MariaDBPort: 3310, PHPMyAdmin: "/pma"}
	os.MkdirAll(s.conf(), 0o755)
	if err := s.writePMAConfig(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(s.conf(), "phpmyadmin.inc.php"))
	conf := string(b)
	if !strings.Contains(conf, "['port'] = '3310'") || !strings.Contains(conf, `O\'Brien\'s web`) {
		t.Errorf("config:\n%s", conf)
	}
	if php, err := exec.LookPath("php"); err == nil {
		if out, err := exec.Command(php, "-l", filepath.Join(s.conf(), "phpmyadmin.inc.php")).CombinedOutput(); err != nil {
			t.Errorf("the config isn't valid PHP: %s", out)
		}
	}
	secret, _ := os.ReadFile(filepath.Join(s.conf(), "phpmyadmin-secret.txt"))
	s.writePMAConfig()
	again, _ := os.ReadFile(filepath.Join(s.conf(), "phpmyadmin-secret.txt"))
	if len(secret) != 64 || string(secret) != string(again) {
		t.Errorf("the cookie secret should be made once and kept: %q then %q", secret, again)
	}
	if _, err := os.Stat(s.PMACustom()); err != nil {
		t.Errorf("the user's phpMyAdmin file wasn't created: %v", err)
	}
}

func TestPHPString(t *testing.T) {
	for in, want := range map[string]string{`C:\xampp`: `'C:\\xampp'`, "it's": `'it\'s'`, "plain": "'plain'"} {
		if got := phpString(in); got != want {
			t.Errorf("phpString(%q) = %s, want %s", in, got, want)
		}
	}
}
