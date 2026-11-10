package fetch

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for name, body := range files {
		f, _ := w.Create(name)
		f.Write([]byte(body))
	}
	w.Close()
	return b.Bytes()
}

func tarGzOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	return b.Bytes()
}

func sum256(b []byte) string { s := sha256.Sum256(b); return "sha256:" + hex.EncodeToString(s[:]) }
func sum512(b []byte) string { s := sha512.Sum512(b); return "sha512:" + hex.EncodeToString(s[:]) }

func serve(t *testing.T, files map[string][]byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestInstallZipAndTarGz(t *testing.T) {
	z := zipOf(t, map[string]string{"apache-tomcat-10/bin/catalina.sh": "#!/bin/sh", "apache-tomcat-10/conf/server.xml": "<Server/>"})
	tg := tarGzOf(t, map[string]string{"jdk-21/bin/java": "binary"})
	srv := serve(t, map[string][]byte{"/tomcat.zip": z, "/jre.tar.gz": tg})
	dir := t.TempDir()

	var reports int
	err := Install(context.Background(), Item{Name: "Tomcat", URLs: []string{srv.URL + "/missing.zip", srv.URL + "/tomcat.zip"},
		Checksum: sum512(z), Strip: 1}, filepath.Join(dir, "tomcat"), func(done, total int64) { reports++ })
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "tomcat", "conf", "server.xml")); string(b) != "<Server/>" || reports == 0 {
		t.Errorf("unpacked server.xml %q, progress reports %d", b, reports)
	}
	if err := Install(context.Background(), Item{Name: "JRE", URLs: []string{srv.URL + "/jre.tar.gz"}, Checksum: sum256(tg), Strip: 1},
		filepath.Join(dir, "jre"), nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "jre", "bin", "java"))
	if err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Errorf("java should be executable: %v %v", info.Mode(), err)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".download-*")); len(left) > 0 {
		t.Errorf("downloads left behind: %v", left)
	}
}

func TestBadChecksumInstallsNothing(t *testing.T) {
	z := zipOf(t, map[string]string{"x/a.txt": "a"})
	srv := serve(t, map[string][]byte{"/a.zip": z})
	dir := t.TempDir()
	dest := filepath.Join(dir, "thing")
	os.MkdirAll(dest, 0o755)
	os.WriteFile(filepath.Join(dest, "keep.txt"), []byte("old"), 0o644)
	err := Install(context.Background(), Item{Name: "Thing", URLs: []string{srv.URL + "/a.zip"}, Checksum: "sha256:" + strings.Repeat("0", 64), Strip: 1}, dest, nil)
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "keep.txt")); string(b) != "old" {
		t.Error("a failed download changed what was installed")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("leftovers: %v", entries)
	}
}

func TestArchiveCantWriteOutsideTheFolder(t *testing.T) {
	z := zipOf(t, map[string]string{"top/../../evil.txt": "x"})
	srv := serve(t, map[string][]byte{"/evil.zip": z})
	dir := t.TempDir()
	err := Install(context.Background(), Item{Name: "Evil", URLs: []string{srv.URL + "/evil.zip"}, Checksum: sum256(z), Strip: 1},
		filepath.Join(dir, "sub", "dest"), nil)
	if err == nil {
		t.Error("an archive escaping its folder was installed")
	}
	if _, err := os.Stat(filepath.Join(dir, "evil.txt")); err == nil {
		t.Error("evil.txt was written outside the folder")
	}
}

func TestReinstallReplaces(t *testing.T) {
	v1 := zipOf(t, map[string]string{"t/old.txt": "1"})
	v2 := zipOf(t, map[string]string{"t/new.txt": "2"})
	srv := serve(t, map[string][]byte{"/v1.zip": v1, "/v2.zip": v2})
	dest := filepath.Join(t.TempDir(), "t")
	Install(context.Background(), Item{Name: "v1", URLs: []string{srv.URL + "/v1.zip"}, Checksum: sum256(v1), Strip: 1}, dest, nil)
	if err := Install(context.Background(), Item{Name: "v2", URLs: []string{srv.URL + "/v2.zip"}, Checksum: sum256(v2), Strip: 1}, dest, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "old.txt")); err == nil {
		t.Error("the old version's files are still there")
	}
	if _, err := os.Stat(filepath.Join(dest, "new.txt")); err != nil {
		t.Error("the new version isn't there")
	}
}
