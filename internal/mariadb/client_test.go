package mariadb

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestClientConfig(t *testing.T) {
	s := &Server{Port: 3307, Socket: `/Users/a b/Library/Application Support/Keel/mysql.sock`}
	got := s.ClientConfig()
	if !strings.Contains(got, "[client]\nport=3307\n") {
		t.Errorf("no [client] port in:\n%s", got)
	}
	if runtime.GOOS == "windows" {
		if strings.Contains(got, "socket=") {
			t.Errorf("a socket on Windows:\n%s", got)
		}
	} else if !strings.Contains(got, `socket="/Users/a b/Library/Application Support/Keel/mysql.sock"`+"\n") {
		t.Errorf("socket not quoted in:\n%s", got)
	}
	if strings.Contains(got, "skip-ssl") {
		t.Errorf("skip-ssl with TLS on:\n%s", got)
	}
	s.NoTLS = true
	if !strings.Contains(s.ClientConfig(), "\nskip-ssl\n") {
		t.Errorf("no skip-ssl with TLS off:\n%s", s.ClientConfig())
	}
	if got := optionValue(`C:\Keel "x"`); got != `"C:\\Keel \"x\""` {
		t.Errorf("optionValue = %s", got)
	}
}

func TestNoTLSStartsWithoutTLS(t *testing.T) {
	s := &Server{DataDir: "/d", Port: 1, Socket: "/s", LogFile: "/l"}
	for _, a := range s.args() {
		if a == "--skip-ssl" {
			t.Fatal("--skip-ssl with TLS on")
		}
	}
	s.NoTLS = true
	args := s.args()
	if args[0] != "--no-defaults" || args[len(args)-1] != "--skip-ssl" {
		t.Errorf("args = %q", args)
	}
}

// client runs MariaDB's command-line client the way Keel's terminal does:
// with MARIADB_HOME pointing at the client settings (when dir isn't
// empty), and a home folder without a ~/.my.cnf.
func client(t *testing.T, s *Server, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(filepath.Join(s.BinDir, exe("mariadb")), args...)
	cmd.Env = append(os.Environ(), "HOME="+t.TempDir(), "MARIADB_HOME="+dir, "MYSQL_HOME=")
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func TestClientConfigReachesTheServer(t *testing.T) {
	s := startTestServer(t)
	if _, err := os.Stat(filepath.Join(s.BinDir, exe("mariadb"))); err != nil {
		t.Skip("no mariadb client beside mariadbd")
	}
	dir := t.TempDir()
	if err := s.WriteClientConfig(dir); err != nil {
		t.Fatal(err)
	}
	// No host or port given, as a student would type it: the settings
	// lead to this server (its socket on macOS and Linux, its port on
	// Windows), not to a system MariaDB.
	out, err := client(t, s, dir, "-u", "root", "-N", "-e", "select @@port")
	if err != nil || out != strconv.Itoa(s.Port) {
		t.Fatalf("mariadb -u root: %v: %s (want port %d)", err, out, s.Port)
	}
}

// Windows on ARM runs MariaDB without TLS. A student who set a root
// password can still log in with MariaDB's own client, thanks to the
// client settings; without them the client refuses.
func TestNoTLSPasswordLogin(t *testing.T) {
	s := newTestServer(t)
	s.NoTLS = true
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopForCleanup(t, s) })
	if _, err := os.Stat(filepath.Join(s.BinDir, exe("mariadb"))); err != nil {
		t.Skip("no mariadb client beside mariadbd")
	}
	dir := t.TempDir()
	if err := s.WriteClientConfig(dir); err != nil {
		t.Fatal(err)
	}
	if out, err := client(t, s, dir, "-u", "root", "-e", "ALTER USER root@localhost IDENTIFIED BY 'pw1'"); err != nil {
		t.Fatalf("set a password: %v: %s", err, out)
	}
	t.Cleanup(func() {
		client(t, s, dir, "-u", "root", "-ppw1", "-e", "ALTER USER root@localhost IDENTIFIED BY ''")
	})

	out, err := client(t, s, dir, "-u", "root", "-ppw1", "-h", "127.0.0.1", "-N", "-e", "select @@have_ssl")
	if err != nil || out != "DISABLED" {
		t.Fatalf("password login with the settings: %v: %s", err, out)
	}
	out, err = client(t, s, "", "-u", "root", "-ppw1", "-h", "127.0.0.1", "-P", strconv.Itoa(s.Port), "-e", "select 1")
	if err == nil {
		t.Logf("this client logs in without TLS even without the settings: %s", out)
	} else if !strings.Contains(out, "SSL is required") {
		t.Errorf("without the settings: %v: %s", err, out)
	}
}
