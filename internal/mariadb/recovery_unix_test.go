//go:build !windows

package mariadb

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFailedRecoveryReusesFirstSnapshot(t *testing.T) {
	// A MariaDB that can't start. Each failed attempt must keep the copy
	// from before the first attempt, not pile up new ones.
	s := newTestServer(t)
	s.setJournal(stateKilled, 0)
	fakeBin := t.TempDir()
	script := "#!/bin/sh\necho '[ERROR] Aria recovery failed (pretend)' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(fakeBin, "mariadbd"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	s.BinDir = fakeBin

	var first string
	for attempt := 1; attempt <= 3; attempt++ {
		err := s.Start(context.Background())
		if err == nil {
			t.Fatalf("attempt %d: the fake MariaDB started", attempt)
		}
		if !strings.Contains(err.Error(), "Aria recovery failed (pretend)") {
			t.Errorf("attempt %d: the error should quote MariaDB's log: %v", attempt, err)
		}
		j, _ := readJournal(s.journalFile())
		if j.Snapshot == "" {
			t.Fatalf("attempt %d: the journal lost the snapshot", attempt)
		}
		if first == "" {
			first = j.Snapshot
		}
		if j.Snapshot != first || !strings.Contains(err.Error(), first) {
			t.Errorf("attempt %d: snapshot %q (error: %v), want the first one %q", attempt, j.Snapshot, err, first)
		}
	}
	snaps, _ := filepath.Glob(filepath.Join(s.backupDir(), snapshotPrefix+"*"))
	if len(snaps) != 1 {
		t.Errorf("%d snapshots after 3 failed attempts, want 1", len(snaps))
	}
}

func TestStartRefusesWhenJournalCantBeWritten(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores folder permissions")
	}
	s := newTestServer(t)
	readOnly := t.TempDir()
	os.WriteFile(filepath.Join(readOnly, "j.json"), []byte(`{"state":"stopped"}`), 0o644)
	os.Chmod(readOnly, 0o500)
	t.Cleanup(func() { os.Chmod(readOnly, 0o700) })
	s.JournalFile = filepath.Join(readOnly, "j.json")

	err := s.Start(context.Background())
	if err == nil {
		stopForCleanup(t, s)
		t.Fatal("Start succeeded without being able to write the journal")
	}
	if !strings.Contains(err.Error(), "wasn't started") {
		t.Errorf("error should say MariaDB wasn't started: %v", err)
	}
	if s.RunningPID() != 0 {
		t.Error("MariaDB is running even though Start failed")
	}
}
