package mariadb

import (
	"context"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// setRootPassword gives every root account a password, as course
// instructions often have students do.
func setRootPassword(t *testing.T, s *Server, password string) {
	t.Helper()
	db := openDB(t, s)
	defer db.Close()
	rows, err := db.Query("SELECT user, host FROM mysql.global_priv WHERE user = 'root'")
	if err != nil {
		t.Fatal(err)
	}
	var accounts []string
	for rows.Next() {
		var user, host string
		rows.Scan(&user, &host)
		accounts = append(accounts, quoteIdent(user)+"@"+quoteIdent(host))
	}
	rows.Close()
	for _, a := range accounts {
		mustExec(t, db, "ALTER USER "+a+" IDENTIFIED BY '"+password+"'")
	}
}

// loggedInAs is the user Keel logs in as now.
func loggedInAs(t *testing.T, s *Server) string {
	t.Helper()
	db, user, err := s.connect(context.Background(), s.Port)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	db.Close()
	return user
}

// With Keel's own account, a root password changes nothing: Stop works on
// the first request, Start confirms the server, and the tables can be
// checked (which recovery after a crash depends on).
func TestRootPasswordDoesntStopKeel(t *testing.T) {
	s := startTestServer(t)
	info, err := os.Stat(s.accountFile())
	if err != nil {
		t.Fatalf("no account file after the first start: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("account file mode %v, want 0600", info.Mode().Perm())
	}
	if u := loggedInAs(t, s); u != keelUser {
		t.Errorf("logged in as %s, want %s", u, keelUser)
	}
	setRootPassword(t, s, "class-password")

	res, err := s.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if res.Nudged || !res.Confirmed {
		t.Errorf("Stop = %+v, want the first request to work and a confirmed shutdown", res)
	}
	began := time.Now()
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start with a root password: %v", err)
	}
	if took := time.Since(began); took > 15*time.Second {
		t.Errorf("Start took %v", took)
	}
	if checked, problems, err := s.CheckTables(context.Background()); err != nil || checked == 0 || len(problems) > 0 {
		t.Errorf("CheckTables = %d, %v, %v", checked, problems, err)
	}
}

// Without any login (root has a password and Keel's account is gone, as
// with a data folder from an older Keel): Start still confirms the server
// from its log, checking the tables says why it can't, and Stop still
// shuts MariaDB down cleanly, with SIGTERM on macOS and Linux and with
// MariaDB's shutdown event on Windows.
func TestNoLogin(t *testing.T) {
	s := startTestServer(t)
	db := openDB(t, s)
	mustExec(t, db, "DROP USER "+keelUser+"@'"+keelHost+"'")
	db.Close()
	os.Remove(s.accountFile())
	setRootPassword(t, s, "class-password")

	exited := s.Exited()
	res, err := s.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !res.Nudged {
		t.Error("expected Stop to fall back to SIGTERM, or the shutdown event on Windows")
	}
	if !res.Confirmed {
		t.Error(`the fallback should still give a clean "Shutdown complete"`)
	}
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Error("the server didn't exit")
	}

	var msgs []string
	s.Progress = func(m string) { msgs = append(msgs, m) }
	began := time.Now()
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if took := time.Since(began); took > 15*time.Second {
		t.Errorf("Start took %v", took)
	}
	if !strings.Contains(strings.Join(msgs, "\n"), "root has a password") {
		t.Errorf("Start didn't say why Keel can't log in:\n%s", strings.Join(msgs, "\n"))
	}
	if _, err := os.Stat(s.accountFile()); err == nil {
		t.Error("an account file without an account")
	}
	if _, _, err := s.CheckTables(context.Background()); !errors.Is(err, ErrNoLogin) {
		t.Errorf("CheckTables = %v, want ErrNoLogin", err)
	}
	if res, err := s.Stop(context.Background()); err != nil || !res.Confirmed {
		t.Errorf("second Stop = %+v, %v", res, err)
	}
}

// A lost or wrong account password is replaced on the next start, while
// root still has none.
func TestAccountIsRemade(t *testing.T) {
	s := startTestServer(t)
	if err := os.WriteFile(s.accountFile(), []byte("wrong\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if u := loggedInAs(t, s); u != "root" {
		t.Fatalf("logged in as %s with a wrong account password", u)
	}
	if _, err := s.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if u := loggedInAs(t, s); u != keelUser {
		t.Errorf("after a restart, logged in as %s, want %s", u, keelUser)
	}
}
