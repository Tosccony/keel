// Package fetch downloads the parts Keel installs when they're first turned
// on (Tomcat, a Java runtime, Perl on Windows): too big to put in every
// installer, and not needed by most students.
//
// Every download is checked against a pinned checksum before it's used,
// and unpacked under a temporary name that's renamed into place only when
// complete, so a half-finished download is never mistaken for a good one.
package fetch

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Item is one download.
type Item struct {
	Name string // for messages: "Tomcat 10.1.48"
	URLs []string
	// Checksum is "sha256:<hex>" or "sha512:<hex>".
	Checksum string
	// Strip removes this many leading folders from the archive's paths,
	// like tar's --strip-components: archives usually hold one top folder.
	Strip int
}

// Progress reports bytes downloaded so far, and the total if known (else 0).
type Progress func(done, total int64)

// Install downloads it, checks it, and unpacks it to dest, replacing what
// was there. dest's parent folder must be writable.
func Install(ctx context.Context, it Item, dest string, progress Progress) error {
	parent := filepath.Dir(dest)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	archive, err := download(ctx, it, parent, progress)
	if err != nil {
		return err
	}
	defer os.Remove(archive)

	tmp := dest + ".partial"
	os.RemoveAll(tmp)
	if err := unpack(archive, tmp, it.Strip); err != nil {
		os.RemoveAll(tmp)
		return fmt.Errorf("unpack %s: %w", it.Name, err)
	}
	old := dest + ".old"
	os.RemoveAll(old)
	if _, err := os.Stat(dest); err == nil {
		if err := os.Rename(dest, old); err != nil {
			os.RemoveAll(tmp)
			return err
		}
	}
	if err := os.Rename(tmp, dest); err != nil {
		os.Rename(old, dest)
		os.RemoveAll(tmp)
		return err
	}
	os.RemoveAll(old)
	return nil
}

// download fetches the first URL that works into a file in dir and checks
// it.
func download(ctx context.Context, it Item, dir string, progress Progress) (string, error) {
	algo, want, ok := strings.Cut(it.Checksum, ":")
	if !ok {
		return "", fmt.Errorf("%s: no checksum", it.Name)
	}
	var lastErr error
	for _, url := range it.URLs {
		path, err := downloadOne(ctx, url, dir, algo, want, progress)
		if err == nil {
			return path, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			break
		}
	}
	return "", fmt.Errorf("download %s: %w", it.Name, lastErr)
}

func downloadOne(ctx context.Context, url, dir, algo, want string, progress Progress) (string, error) {
	var h hash.Hash
	switch algo {
	case "sha256":
		h = sha256.New()
	case "sha512":
		h = sha512.New()
	default:
		return "", fmt.Errorf("unknown checksum type %q", algo)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Keel (https://github.com/Tosccony/keel)")
	resp, err := (&http.Client{Timeout: 30 * time.Minute}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: %s", url, resp.Status)
	}
	f, err := os.CreateTemp(dir, ".download-*")
	if err != nil {
		return "", err
	}
	keep := false
	defer func() {
		f.Close()
		if !keep {
			os.Remove(f.Name())
		}
	}()
	r := io.Reader(resp.Body)
	if progress != nil {
		r = &counter{r: resp.Body, total: resp.ContentLength, report: progress}
	}
	if _, err := io.Copy(io.MultiWriter(f, h), r); err != nil {
		return "", err
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, want) {
		return "", fmt.Errorf("%s doesn't match its checksum (got %s…)", url, got[:16])
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	keep = true
	return f.Name(), nil
}

// counter reports progress at most a few times a second.
type counter struct {
	r      io.Reader
	done   int64
	total  int64
	last   time.Time
	report Progress
}

func (c *counter) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.done += int64(n)
	if time.Since(c.last) > 250*time.Millisecond || err == io.EOF {
		c.last = time.Now()
		c.report(c.done, max(c.total, 0))
	}
	return n, err
}

// unpack extracts a .zip or .tar.gz into dest, refusing any entry that
// would land outside it.
func unpack(archive, dest string, strip int) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	magic := make([]byte, 4)
	io.ReadFull(f, magic)
	f.Seek(0, io.SeekStart)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	if string(magic) == "PK\x03\x04" {
		info, _ := f.Stat()
		return unzip(f, info.Size(), dest, strip)
	}
	return untar(f, dest, strip)
}

// target maps an archive path into dest after stripping folders, or ""
// for an entry to skip.
func target(dest, name string, strip int) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	parts := strings.Split(strings.Trim(name, "/"), "/")
	if len(parts) <= strip {
		return "", nil
	}
	rel := filepath.Join(parts[strip:]...)
	full := filepath.Join(dest, rel)
	if r, err := filepath.Rel(dest, full); err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("archive entry %q would land outside the folder", name)
	}
	return full, nil
}

func unzip(f *os.File, size int64, dest string, strip int) error {
	zr, err := zip.NewReader(f, size)
	if err != nil {
		return err
	}
	for _, e := range zr.File {
		path, err := target(dest, e.Name, strip)
		if err != nil {
			return err
		}
		if path == "" {
			continue
		}
		if e.FileInfo().IsDir() {
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := writeFile(path, e.Mode(), func() (io.ReadCloser, error) { return e.Open() }); err != nil {
			return err
		}
	}
	return nil
}

func untar(f *os.File, dest string, strip int) error {
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		path, err := target(dest, h.Name, strip)
		if err != nil {
			return err
		}
		if path == "" {
			continue
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := writeFile(path, h.FileInfo().Mode(), func() (io.ReadCloser, error) { return io.NopCloser(tr), nil }); err != nil {
				return err
			}
		case tar.TypeSymlink:
			// Only links that stay inside the folder (Java runtimes have some).
			if strings.HasPrefix(h.Linkname, "/") || strings.Contains(h.Linkname, "..") {
				continue
			}
			os.MkdirAll(filepath.Dir(path), 0o755)
			if err := os.Symlink(h.Linkname, path); err != nil {
				return err
			}
		}
	}
}

func writeFile(path string, mode os.FileMode, open func() (io.ReadCloser, error)) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	r, err := open()
	if err != nil {
		return err
	}
	defer r.Close()
	perm := mode.Perm() | 0o600
	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
