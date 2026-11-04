package setup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Tosccony/keel/internal/mariadb"
	"github.com/Tosccony/keel/internal/ports"
)

// settings are choices Keel made and keeps, like a port it moved to.
type settings struct {
	MariaDBPort int `json:"mariadbPort,omitempty"`
}

func (o Options) settingsFile() string { return filepath.Join(o.Folder, "keel-settings.json") }

func loadSettings(path string) (settings, error) {
	var st settings
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	return st, json.Unmarshal(b, &st)
}

// saveSettings writes the file whole or not at all.
func saveSettings(path string, st settings) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// ChoosePort makes sure MariaDB's port is free before a start.
//
// If another program holds it, ChoosePort names that program, moves s to
// the next free port and remembers the new port, so MariaDB stays on it
// from then on instead of wandering between runs. The note says what
// happened, for the user; it's empty when nothing changed. A MariaDB that's
// already running keeps its port.
func (o Options) ChoosePort(ctx context.Context, s *mariadb.Server) (note string, err error) {
	if s.RunningPID() != 0 || ports.Free(s.Port) {
		return "", nil
	}
	holder := "another program"
	if h, ok := ports.Who(ctx, s.Port); ok {
		holder = h.String()
	}
	next, err := ports.Next(s.Port+1, nil)
	if err != nil {
		return "", fmt.Errorf("port %d is in use by %s, and there's %w", s.Port, holder, err)
	}
	note = fmt.Sprintf("Port %d is in use by %s, so MariaDB will use port %d from now on.", s.Port, holder, next)
	if err := saveSettings(o.settingsFile(), settings{MariaDBPort: next}); err != nil {
		note += fmt.Sprintf(" (Keel couldn't save that for next time: %v)", err)
	}
	s.Port = next
	return note, nil
}
