package mariadb

import (
	"bytes"
	"crypto/sha256"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

// makeTree builds a small data-folder-like tree and returns its path.
func makeTree(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "data")
	files := map[string]string{
		"ibdata1":                strings.Repeat("innodb", 50000),
		"aria_log_control":       "control",
		"mysql/global_priv.MAD":  "users",
		"mysql/db.MAI":           "index",
		"shop/orders.ibd":        strings.Repeat("o", 70000),
		"shop/sub/nested/deep.x": "deep",
		"empty-file":             "",
	}
	for name, content := range files {
		path := filepath.Join(root, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	os.MkdirAll(filepath.Join(root, "empty-dir"), 0o755)
	os.Chmod(filepath.Join(root, "mysql/global_priv.MAD"), 0o600)
	return root
}

// treeFingerprint lists every entry with its kind, mode and content hash.
func treeFingerprint(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(root, path)
		info, _ := d.Info()
		switch {
		case d.IsDir():
			out = append(out, rel+"/")
		case info.Mode()&fs.ModeSymlink != 0:
			link, _ := os.Readlink(path)
			out = append(out, rel+" -> "+link)
		case info.Mode().IsRegular():
			b, _ := os.ReadFile(path)
			out = append(out, rel+" "+info.Mode().Perm().String()+" "+string(sha256Sum(b)))
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func sha256Sum(b []byte) []byte {
	h := sha256.Sum256(b)
	return h[:]
}

func TestCopyTreeCopiesEverything(t *testing.T) {
	src := makeTree(t)
	if runtime.GOOS != "windows" {
		os.Symlink("ibdata1", filepath.Join(src, "link-to-ibdata1"))
	}
	dst := filepath.Join(t.TempDir(), "copy")
	if err := copyTree(src, dst); err != nil {
		t.Fatal(err)
	}
	want, got := treeFingerprint(t, src), treeFingerprint(t, dst)
	if strings.Join(want, "\n") != strings.Join(got, "\n") {
		t.Errorf("copy differs from the original:\nwant %q\ngot  %q", want, got)
	}
}

func TestCopyTreeSkipsSockets(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix sockets")
	}
	src, err := os.MkdirTemp("/tmp", "keel-sock-") // socket paths must be short
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(src)
	l, err := net.Listen("unix", filepath.Join(src, "mysql.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	os.WriteFile(filepath.Join(src, "ibdata1"), []byte("data"), 0o640)

	dst := filepath.Join(t.TempDir(), "copy")
	if err := copyTree(src, dst); err != nil {
		t.Fatalf("copying a folder with a socket failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "ibdata1")); err != nil {
		t.Error("the regular file wasn't copied")
	}
	if _, err := os.Lstat(filepath.Join(dst, "mysql.sock")); err == nil {
		t.Error("the socket was copied; it should be skipped")
	}
}

func TestSnapshotIsCompleteAndIndependent(t *testing.T) {
	s := &Server{DataDir: makeTree(t)}
	path, err := s.snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(filepath.Base(path), snapshotPrefix) || filepath.Dir(path) != s.backupDir() {
		t.Errorf("snapshot at %s, want %s/%s…", path, s.backupDir(), snapshotPrefix)
	}
	if strings.Join(treeFingerprint(t, s.DataDir), "\n") != strings.Join(treeFingerprint(t, path), "\n") {
		t.Fatal("the snapshot doesn't match the data folder")
	}
	// Changing the original afterwards (as recovery will) must not change
	// the copy. This matters with copy-on-write clones.
	original := filepath.Join(s.DataDir, "ibdata1")
	before, _ := os.ReadFile(filepath.Join(path, "ibdata1"))
	os.WriteFile(original, []byte("recovered"), 0o640)
	after, _ := os.ReadFile(filepath.Join(path, "ibdata1"))
	if !bytes.Equal(before, after) {
		t.Error("changing the data folder changed the snapshot")
	}
}

func TestSnapshotWithoutCloning(t *testing.T) {
	cloneDir = func(string, string) error { return errNoClone }
	t.Cleanup(func() { cloneDir = cloneTree })
	s := &Server{DataDir: makeTree(t)}
	path, err := s.snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(treeFingerprint(t, s.DataDir), "\n") != strings.Join(treeFingerprint(t, path), "\n") {
		t.Error("the plain-copy snapshot doesn't match the data folder")
	}
}

func TestSnapshotNamesAreUnique(t *testing.T) {
	s := &Server{DataDir: makeTree(t)}
	seen := map[string]bool{}
	for i := 0; i < 3; i++ { // same second, almost certainly
		path, err := s.snapshot()
		if err != nil {
			t.Fatal(err)
		}
		if seen[path] {
			t.Fatalf("two snapshots got the same name: %s", path)
		}
		seen[path] = true
	}
}

func TestSnapshotRefusesWhenDiskIsFull(t *testing.T) {
	cloneDir = func(string, string) error { return errNoClone }
	freeSpace = func(string) (uint64, error) { return 1 << 20, nil } // 1 MB free
	t.Cleanup(func() { cloneDir, freeSpace = cloneTree, diskFree })

	s := &Server{DataDir: makeTree(t)}
	_, err := s.snapshot()
	if err == nil || !strings.Contains(err.Error(), "not enough free space") {
		t.Fatalf("got %v, want a not-enough-space error", err)
	}
	if entries, _ := os.ReadDir(s.backupDir()); len(entries) != 0 {
		t.Errorf("a refused snapshot left %d entries in the backups folder", len(entries))
	}
}

func TestFailedSnapshotLeavesNothing(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs Unix permissions and a non-root user")
	}
	cloneDir = func(string, string) error { return errNoClone }
	t.Cleanup(func() { cloneDir = cloneTree })

	s := &Server{DataDir: makeTree(t)}
	unreadable := filepath.Join(s.DataDir, "shop/orders.ibd")
	os.Chmod(unreadable, 0o000)
	t.Cleanup(func() { os.Chmod(unreadable, 0o640) })

	if _, err := s.snapshot(); err == nil {
		t.Fatal("snapshot of an unreadable file succeeded")
	}
	if entries, _ := os.ReadDir(s.backupDir()); len(entries) != 0 {
		t.Errorf("a failed snapshot left %d entries (half-made copy?)", len(entries))
	}
}

func TestPruneSnapshots(t *testing.T) {
	s := &Server{DataDir: filepath.Join(t.TempDir(), "data")}
	dir := s.backupDir()
	stamp := func(age time.Duration) string { return time.Now().Add(-age).Format(snapshotLayout) }
	day := 24 * time.Hour
	names := map[string]bool{ // name: should survive
		snapshotPrefix + stamp(30*day):             false, // old
		snapshotPrefix + stamp(10*day):             true,  // old, but the journal points at it
		snapshotPrefix + stamp(8*day) + "-2":       false, // old, with a same-second suffix
		snapshotPrefix + stamp(2*day):              true,  // recent
		snapshotPrefix + stamp(time.Hour):          true,  // newest
		snapshotPrefix + stamp(9*day) + ".partial": false, // abandoned half-copy
		snapshotPrefix + stamp(0) + ".partial":     true,  // a copy in progress right now
		"my-notes":                                 true,  // not ours
	}
	for name := range names {
		os.MkdirAll(filepath.Join(dir, name), 0o755)
	}
	old := time.Now().Add(-9 * day)
	os.Chtimes(filepath.Join(dir, snapshotPrefix+stamp(9*day)+".partial"), old, old)
	keep := filepath.Join(dir, snapshotPrefix+stamp(10*day))

	s.pruneSnapshots(keep)
	for name, survive := range names {
		_, err := os.Stat(filepath.Join(dir, name))
		if exists := err == nil; exists != survive {
			t.Errorf("%s: exists=%v, want %v", name, exists, survive)
		}
	}
}

func TestPruneKeepsNewestEvenIfOld(t *testing.T) {
	s := &Server{DataDir: filepath.Join(t.TempDir(), "data")}
	only := filepath.Join(s.backupDir(), snapshotPrefix+time.Now().Add(-60*24*time.Hour).Format(snapshotLayout))
	os.MkdirAll(only, 0o755)
	s.pruneSnapshots("")
	if !dirExists(only) {
		t.Error("pruning deleted the only (newest) snapshot")
	}
}

func TestDirSizeAndHumanBytes(t *testing.T) {
	root := makeTree(t)
	size, err := dirSize(root)
	if err != nil {
		t.Fatal(err)
	}
	if want := int64(len(strings.Repeat("innodb", 50000)) + 7 + 5 + 5 + 70000 + 4); size != want {
		t.Errorf("dirSize = %d, want %d", size, want)
	}
	for n, want := range map[uint64]string{512: "512 B", 2048: "2.0 KB", 5 << 30: "5.0 GB"} {
		if got := humanBytes(n); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestDiskFree(t *testing.T) {
	free, err := diskFree(t.TempDir())
	if err != nil || free == 0 {
		t.Errorf("diskFree = %d, %v", free, err)
	}
}
