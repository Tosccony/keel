package mariadb

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

// Keel's own MariaDB account.
//
// Keel logs in to MariaDB to confirm a start (and that the server on the
// port uses our data folder), to ask it to shut down, and to check the
// tables after a crash. Courses often have students give root a password,
// and a root login without one then fails. So on a start where root has no
// password, Keel adds its own account, keel@127.0.0.1, with a random
// password kept in AccountFile (readable only by the user), and logs in
// with that from then on. Root stays the student's to change.
const (
	keelUser = "keel"
	keelHost = "127.0.0.1" // Keel always connects over TCP to 127.0.0.1
)

// ErrNoLogin means MariaDB answered but refused every login Keel has: root
// has a password, and Keel's own account is missing or has another one.
var ErrNoLogin = errors.New("MariaDB refused Keel's logins (root has a password, and Keel's own account is missing)")

type login struct{ user, password string }

func (s *Server) accountFile() string {
	if s.AccountFile != "" {
		return s.AccountFile
	}
	return filepath.Join(filepath.Dir(s.DataDir), "keel-account")
}

// logins lists the logins to try, best first: Keel's own account if its
// password is stored, then root without a password.
func (s *Server) logins() []login {
	var ls []login
	if b, err := os.ReadFile(s.accountFile()); err == nil {
		if pw := strings.TrimSpace(string(b)); pw != "" {
			ls = append(ls, login{keelUser, pw})
		}
	}
	return append(ls, login{user: "root"})
}

func (s *Server) dsnAs(port int, l login) string {
	cfg := mysql.NewConfig()
	cfg.User, cfg.Passwd = l.user, l.password
	cfg.Net = "tcp"
	cfg.Addr = net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	cfg.Timeout = time.Second
	cfg.ReadTimeout = 10 * time.Second
	cfg.WriteTimeout = 10 * time.Second
	return cfg.FormatDSN()
}

// connect opens a connection pool to the server on port with the first
// login it accepts, and says which user that was. If the server answers
// but refuses them all, the error wraps ErrNoLogin.
func (s *Server) connect(ctx context.Context, port int) (*sql.DB, string, error) {
	var refused error
	for _, l := range s.logins() {
		db, err := sql.Open("mysql", s.dsnAs(port, l))
		if err != nil {
			return nil, "", fmt.Errorf("mariadb: %w", err)
		}
		err = db.PingContext(ctx)
		if err == nil {
			return db, l.user, nil
		}
		db.Close()
		if !accessDenied(err) {
			return nil, "", err
		}
		refused = err
	}
	return nil, "", fmt.Errorf("mariadb: %w: %v", ErrNoLogin, refused)
}

// accessDenied reports whether err is MariaDB refusing a login.
func accessDenied(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && (me.Number == 1045 || me.Number == 1698)
}

// ensureAccount (re)creates Keel's own account with a new random password
// and stores the password. Keel calls it after logging in as root: on the
// first start, and if the account or its stored password went missing (a
// data folder from another computer, say).
func (s *Server) ensureAccount(ctx context.Context, db *sql.DB) error {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	pw := hex.EncodeToString(b) // only hex digits, so safe inside quotes
	account := "'" + keelUser + "'@'" + keelHost + "'"
	for _, stmt := range []string{
		"CREATE USER IF NOT EXISTS " + account + " IDENTIFIED BY '" + pw + "'",
		"ALTER USER " + account + " IDENTIFIED BY '" + pw + "'",
		"GRANT ALL PRIVILEGES ON *.* TO " + account + " WITH GRANT OPTION",
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("mariadb: Keel's own account: %w", err)
		}
	}
	path := s.accountFile()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(pw+"\n"), 0o600); err != nil {
		return fmt.Errorf("mariadb: save Keel's account password: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("mariadb: save Keel's account password: %w", err)
	}
	return nil
}
