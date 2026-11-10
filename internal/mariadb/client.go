package mariadb

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ClientConfigName is the client settings file WriteClientConfig writes.
// MariaDB's commands read it from the folder named by MARIADB_HOME.
const ClientConfigName = "my.cnf"

// ClientConfig is the [client] section that points MariaDB's own commands
// (mariadb, mariadb-dump, mariadb-admin) at this server: its port, its
// socket on macOS and Linux (without it they look for the system's
// MariaDB), and no TLS when the server runs without it.
func (s *Server) ClientConfig() string {
	var b strings.Builder
	b.WriteString("# Written by Keel so MariaDB's commands in Keel's terminal reach Keel's\n")
	b.WriteString("# MariaDB. Keel rewrites it; put your own settings in ~/.my.cnf.\n")
	b.WriteString("[client]\n")
	fmt.Fprintf(&b, "port=%d\n", s.Port)
	if runtime.GOOS != "windows" && s.Socket != "" {
		fmt.Fprintf(&b, "socket=%s\n", optionValue(s.Socket))
	}
	if s.NoTLS {
		b.WriteString("skip-ssl\n")
	}
	return b.String()
}

// WriteClientConfig writes ClientConfig to dir/my.cnf. Run MariaDB's
// commands with MARIADB_HOME=dir to use it.
func (s *Server) WriteClientConfig(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mariadb: client settings: %w", err)
	}
	path := filepath.Join(dir, ClientConfigName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(s.ClientConfig()), 0o644); err != nil {
		return fmt.Errorf("mariadb: client settings: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("mariadb: client settings: %w", err)
	}
	return nil
}

// optionValue quotes a value for a MariaDB option file, where a backslash
// starts an escape.
func optionValue(v string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(v, `\`, `\\`), `"`, `\"`) + `"`
}
