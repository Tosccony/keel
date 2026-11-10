package mariadb

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// upgradeInfoName is the file in the data folder where mariadb-install-db
// and mariadb-upgrade record which MariaDB the system tables were made for.
const upgradeInfoName = "mariadb_upgrade_info"

// upgradeIfNeeded runs mariadb-upgrade when the running MariaDB is newer
// than the one the data folder's system tables were made for, as after an
// update of Keel brings a newer MariaDB. It only reports problems: MariaDB
// keeps running either way.
func (s *Server) upgradeIfNeeded(ctx context.Context) {
	db, l, err := s.connect(ctx, s.Port)
	if err != nil {
		return
	}
	var running string
	err = db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&running)
	db.Close()
	if err != nil {
		return
	}
	b, err := os.ReadFile(filepath.Join(s.DataDir, upgradeInfoName))
	madeFor := strings.TrimSpace(strings.TrimRight(string(b), "\x00"))
	if err == nil && !newerVersion(running, madeFor) {
		return
	}
	if madeFor == "" {
		madeFor = "an older MariaDB"
	}
	s.progress(fmt.Sprintf("MariaDB was updated (the data folder was made for %s, this is %s). Updating its system tables…", versionNumber(madeFor), versionNumber(running)))
	if err := s.runUpgrade(ctx, l); err != nil {
		s.progress("Couldn't update the system tables: " + err.Error())
		return
	}
	s.progress("System tables updated.")
}

// runUpgrade runs mariadb-upgrade, logging in as l through a settings file
// only the user can read, so the password never shows in the process list.
func (s *Server) runUpgrade(ctx context.Context, l login) error {
	f, err := os.CreateTemp(filepath.Dir(s.DataDir), ".keel-upgrade-*.cnf") // made readable by the user only
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	settings := fmt.Sprintf("[client]\nuser=%s\npassword=%s\nhost=127.0.0.1\nport=%d\n", optionValue(l.user), optionValue(l.password), s.Port)
	if s.NoTLS {
		settings += "skip-ssl\n"
	}
	_, err = f.WriteString(settings)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	// --defaults-file must come first, and reads only this file: no my.cnf
	// elsewhere on the computer gets a say.
	cmd := exec.CommandContext(ctx, filepath.Join(s.BinDir, exe("mariadb-upgrade")), "--defaults-file="+f.Name())
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mariadb-upgrade: %w\n%s", err, lastLines(string(out), 10))
	}
	return nil
}

// versionNumber is the "11.8.9" in "11.8.9-MariaDB-log".
func versionNumber(v string) string {
	if i := strings.IndexFunc(v, func(r rune) bool { return r != '.' && (r < '0' || r > '9') }); i >= 0 {
		return v[:i]
	}
	return v
}

// newerVersion reports whether version a is newer than b, comparing their
// numbers part by part.
func newerVersion(a, b string) bool {
	pa, pb := strings.Split(versionNumber(a), "."), strings.Split(versionNumber(b), ".")
	for i := range max(len(pa), len(pb)) {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			return x > y
		}
	}
	return false
}
