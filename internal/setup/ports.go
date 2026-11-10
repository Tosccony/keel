package setup

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/Tosccony/keel/internal/mariadb"
	"github.com/Tosccony/keel/internal/ports"
)

func (o Options) settingsFile() string { return filepath.Join(o.home(), "keel-settings.json") }

// Settings returns the settings kept in Keel's folder.
func (o Options) Settings() *Settings { return OpenSettings(o.settingsFile()) }

// MovePort makes sure port is free for a part of Keel. If another program
// holds it, MovePort names that program, picks the next free port that no
// other part of Keel is set to use, remembers it, and returns it with a
// note for the user. The note is empty when nothing changed. name is the
// part's name for the note, such as "MariaDB" or "the web server".
func (o Options) MovePort(ctx context.Context, part, name string, port int) (int, string, error) {
	if ports.Free(port) {
		return port, "", nil
	}
	holder := "another program"
	if h, ok := ports.Who(ctx, port); ok {
		holder = h.String()
	}
	st := o.Settings()
	next, err := ports.Next(port+1, st.takenByOthers(part))
	if err != nil {
		return port, "", fmt.Errorf("port %d is in use by %s, and there's %w", port, holder, err)
	}
	note := fmt.Sprintf("Port %d is in use by %s, so %s will use port %d from now on.", port, holder, name, next)
	if err := st.SetPort(part, next); err != nil {
		note += fmt.Sprintf(" (Keel couldn't save that for next time: %v)", err)
	}
	return next, note, nil
}

// ChoosePort makes sure MariaDB's port is free before a start.
//
// If another program holds it, ChoosePort names that program, moves s to
// the next free port and remembers the new port, so MariaDB stays on it
// from then on instead of wandering between runs. The note says what
// happened, for the user; it's empty when nothing changed. A MariaDB that's
// already running keeps its port.
func (o Options) ChoosePort(ctx context.Context, s *mariadb.Server) (note string, err error) {
	if s.RunningPID() != 0 {
		return "", nil
	}
	port, note, err := o.MovePort(ctx, PartMariaDB, "MariaDB", s.Port)
	if err != nil {
		return "", err
	}
	s.Port = port
	return note, nil
}
