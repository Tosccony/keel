// Package web runs Keel's web server: Apache with PHP.
//
// On macOS and Linux, PHP runs as PHP-FPM behind Apache. On Windows it's
// loaded into Apache as a module, the way XAMPP does it. Either way, Keel
// writes the config at every start (ports and folders) and includes files
// the user owns for their own settings.
package web

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Tosccony/keel/internal/proc"
)

// Runtime is the Apache and PHP that Keel runs.
type Runtime struct {
	Httpd        string // httpd
	ModulesDir   string // folder of mod_*.so files to load; "" when they're built in
	MimeTypes    string // Apache's mime.types
	PHP          string // the PHP command line, for the version and the terminal
	PHPFPM       string // php-fpm (macOS, Linux); "" on Windows
	PHPModule    string // php8apache2_4.dll (Windows); "" elsewhere
	ExtensionDir string // PHP's extension folder, when extensions are separate files
}

// Server is Apache with PHP, serving Sites.
type Server struct {
	Runtime
	Folder string // Keel's web folder: conf/, logs/, run/, tmp/
	Sites  string // the websites folder (DocumentRoot)

	Port          int    // HTTP
	HTTPSPort     int    // HTTPS, with a certificate Keel makes (CertFile); 0 for none
	FPMPort       int    // PHP-FPM, on 127.0.0.1
	MariaDBPort   int    // for PHP's defaults
	MariaDBSocket string // for PHP's "localhost" (macOS, Linux)
	MailPort      int    // the mail catcher, for PHP on Windows
	MailInbox     string // the mail catcher's inbox page, for PHP code
	Sendmail      string // the sendmail stand-in, for PHP on macOS and Linux
	Timezone      string // IANA name for date.timezone; "" leaves PHP's default
	PHPMyAdmin    string // phpMyAdmin's folder, served at /phpmyadmin; "" for none
	Perl          string // the perl program, to run .pl and .cgi files as CGI; "" for none

	ReadyTimeout time.Duration // default 15s

	httpd, fpm *proc.Process
	token      string // in a header of every reply, so Keel knows it's its own Apache answering
}

func (s *Server) conf() string     { return filepath.Join(s.Folder, "conf") }
func (s *Server) logs() string     { return filepath.Join(s.Folder, "logs") }
func (s *Server) run() string      { return filepath.Join(s.Folder, "run") }
func (s *Server) tmp() string      { return filepath.Join(s.Folder, "tmp") }
func (s *Server) sessions() string { return filepath.Join(s.Folder, "tmp", "sessions") }

// ErrorLog is Apache's error log; PHPErrorLog is PHP's.
func (s *Server) ErrorLog() string    { return filepath.Join(s.logs(), "apache-error.log") }
func (s *Server) PHPErrorLog() string { return filepath.Join(s.logs(), "php-errors.log") }

// PMACustom is phpMyAdmin's settings file the user owns.
func (s *Server) PMACustom() string { return filepath.Join(s.conf(), "phpmyadmin-custom.inc.php") }

// CustomConf and CustomIni are the files the user owns.
func (s *Server) CustomConf() string { return filepath.Join(s.conf(), "custom.conf") }
func (s *Server) CustomIni() string  { return filepath.Join(s.conf(), "php.d", "custom.ini") }

func (s *Server) processes() {
	if s.httpd == nil {
		conf := filepath.Join(s.conf(), "httpd.conf")
		s.httpd = &proc.Process{
			Path:    s.Httpd,
			Args:    httpdArgs(conf),
			LogFile: filepath.Join(s.logs(), "apache-output.log"),
			PIDFile: filepath.Join(s.run(), "httpd.pid"),
			Marker:  filepath.ToSlash(conf),
		}
	}
	// On Windows, PHP's extensions need the DLLs in PHP's folder (ICU for
	// intl, OpenSSL, libsodium), and Perl its own, all found on the PATH.
	// It's set each time, as Perl may have been turned on since.
	if s.PHPModule != "" {
		path := filepath.Dir(s.PHPModule)
		if s.Perl != "" {
			path += string(os.PathListSeparator) + filepath.Dir(s.Perl)
		}
		s.httpd.Env = []string{"PATH=" + path + string(os.PathListSeparator) + os.Getenv("PATH")}
	}
	if s.fpm == nil && s.PHPFPM != "" {
		conf := filepath.Join(s.conf(), "php-fpm.conf")
		s.fpm = &proc.Process{
			Path:    s.PHPFPM,
			Args:    []string{"-F", "-y", conf, "-c", filepath.Join(s.conf(), "php.ini")},
			Env:     []string{"PHP_INI_SCAN_DIR=" + filepath.Join(s.conf(), "php.d")},
			LogFile: filepath.Join(s.logs(), "php-fpm-output.log"),
			PIDFile: filepath.Join(s.run(), "php-fpm.pid"),
			Marker:  conf,
		}
	}
}

// httpdArgs runs Apache in the foreground, as a child Keel can watch.
// Windows' httpd.exe stays in the foreground anyway.
func httpdArgs(conf string) []string {
	args := []string{"-f", filepath.ToSlash(conf)}
	if filepath.Separator == '/' {
		args = append(args, "-DFOREGROUND")
	}
	return args
}

// Running reports whether Apache is running.
func (s *Server) Running() bool {
	s.processes()
	return s.httpd.PID() != 0
}

// Start writes the config, checks it, and starts PHP-FPM and Apache. It
// returns once Apache answers, or with an error quoting the logs.
func (s *Server) Start(ctx context.Context) error {
	if s.Running() {
		return nil
	}
	if info, err := os.Stat(s.Sites); err != nil || !info.IsDir() {
		return fmt.Errorf("the websites folder %s isn't there", s.Sites)
	}
	s.token = newToken()
	if err := s.writeConfig(); err != nil {
		return fmt.Errorf("write the web server's config: %w", err)
	}
	if err := s.check(); err != nil {
		return err
	}
	if s.fpm != nil && s.fpm.PID() == 0 {
		if err := s.fpm.Start(); err != nil {
			return fmt.Errorf("start PHP: %w", err)
		}
		if err := s.waitPort(ctx, s.fpm, s.FPMPort, s.fpm.LogFile, filepath.Join(s.logs(), "php-fpm.log")); err != nil {
			s.fpm.Stop(context.Background(), 5*time.Second)
			return fmt.Errorf("PHP didn't start: %w", err)
		}
	}
	if err := s.httpd.Start(); err != nil {
		s.stopFPM()
		return fmt.Errorf("start Apache: %w", err)
	}
	if err := s.waitApache(ctx); err != nil {
		s.httpd.Stop(context.Background(), 5*time.Second)
		s.stopFPM()
		return fmt.Errorf("Apache didn't start: %w", err)
	}
	return nil
}

// check runs Apache's own config test, so a mistake in custom.conf comes
// back as a clear message instead of a server that won't start.
func (s *Server) check() error {
	out, err := exec.Command(s.Httpd, "-t", "-f", filepath.ToSlash(filepath.Join(s.conf(), "httpd.conf"))).CombinedOutput()
	if err == nil {
		return nil
	}
	msg := strings.TrimSpace(string(out))
	return fmt.Errorf("Apache's settings have a mistake (check %s):\n%s", s.CustomConf(), msg)
}

// waitPort waits until something accepts connections on port, the process
// exits, or the wait times out.
func (s *Server) waitPort(ctx context.Context, p *proc.Process, port int, logs ...string) error {
	timeout := s.ReadyTimeout
	if timeout == 0 {
		timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	for {
		if c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond); err == nil {
			c.Close()
			return nil
		}
		select {
		case <-p.Exited():
			return fmt.Errorf("it exited (%v)\n%s", p.ExitErr(), tails(logs...))
		case <-ctx.Done():
			return fmt.Errorf("nothing answered on port %d\n%s", port, tails(logs...))
		case <-tick.C:
		}
	}
}

// waitApache waits until Apache itself answers: a reply carrying this
// start's token. Another program answering on the port doesn't count.
func (s *Server) waitApache(ctx context.Context) error {
	timeout := s.ReadyTimeout
	if timeout == 0 {
		timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client := &http.Client{Timeout: time.Second}
	url := "http://127.0.0.1:" + strconv.Itoa(s.Port) + "/"
	someoneElse := false
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
		if resp, err := client.Do(req); err == nil {
			resp.Body.Close()
			if resp.Header.Get("X-Keel-Server") == s.token {
				return nil
			}
			someoneElse = true
		}
		select {
		case <-s.httpd.Exited():
			return fmt.Errorf("it exited (%v)\n%s", s.httpd.ExitErr(), tails(s.httpd.LogFile, s.ErrorLog()))
		case <-ctx.Done():
			if someoneElse {
				return fmt.Errorf("another program answers on port %d", s.Port)
			}
			return fmt.Errorf("nothing answered on port %d\n%s", s.Port, tails(s.httpd.LogFile, s.ErrorLog()))
		case <-tick.C:
		}
	}
}

func newToken() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Stop stops Apache, then PHP-FPM.
func (s *Server) Stop(ctx context.Context) error {
	s.processes()
	err := s.httpd.Stop(ctx, 10*time.Second)
	if errors.Is(err, proc.ErrNotRunning) {
		err = nil
	}
	if ferr := s.stopFPM(); err == nil {
		err = ferr
	}
	return err
}

func (s *Server) stopFPM() error {
	if s.fpm == nil {
		return nil
	}
	if err := s.fpm.Stop(context.Background(), 10*time.Second); err != nil && !errors.Is(err, proc.ErrNotRunning) {
		return err
	}
	return nil
}

// Versions returns Apache's and PHP's versions, like "2.4.69" and
// "8.4.27", or "" for one that can't be read.
func (s *Server) Versions() (apache, php string) {
	if out, err := exec.Command(s.Httpd, "-v").Output(); err == nil {
		// "Server version: Apache/2.4.69 (Unix)"
		if _, v, ok := strings.Cut(string(out), "Apache/"); ok {
			apache, _, _ = strings.Cut(v, " ")
		}
	}
	if s.PHP != "" {
		if out, err := exec.Command(s.PHP, "-n", "-r", "echo PHP_VERSION;").Output(); err == nil {
			php = strings.TrimSpace(string(out))
		}
	}
	return apache, php
}

// tails returns the last lines of each log that has any.
func tails(paths ...string) string {
	var b strings.Builder
	for _, p := range paths {
		lines := lastLines(p, 8)
		if len(lines) == 0 {
			continue
		}
		fmt.Fprintf(&b, "last lines of %s:\n%s\n", p, strings.Join(lines, "\n"))
	}
	return strings.TrimSpace(b.String())
}

func lastLines(path string, n int) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			lines = append(lines, line)
			if len(lines) > n {
				lines = lines[1:]
			}
		}
	}
	return lines
}
