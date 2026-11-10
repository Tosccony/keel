package web

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCertificate(t *testing.T) {
	s := &Server{Folder: t.TempDir()}
	now := time.Now()
	if err := s.ensureCert(now); err != nil {
		t.Fatal(err)
	}
	pair, err := tls.LoadX509KeyPair(s.CertFile(), s.KeyFile())
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(pair.Certificate[0])
	for _, host := range []string{"localhost", "127.0.0.1", "::1"} {
		if err := cert.VerifyHostname(host); err != nil {
			t.Errorf("certificate doesn't cover %s: %v", host, err)
		}
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(s.KeyFile()); info.Mode().Perm() != 0o600 {
			t.Errorf("key file mode %v, want 0600", info.Mode().Perm())
		}
	}

	first, _ := os.ReadFile(s.CertFile())
	if err := s.ensureCert(now.Add(24 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if again, _ := os.ReadFile(s.CertFile()); !bytes.Equal(first, again) {
		t.Error("a good certificate was replaced")
	}
	// Close to expiry: made again.
	if err := s.ensureCert(cert.NotAfter.Add(-certRenew / 2)); err != nil {
		t.Fatal(err)
	}
	renewed, _ := os.ReadFile(s.CertFile())
	if bytes.Equal(first, renewed) {
		t.Error("a certificate about to expire was kept")
	}
	// Damaged: made again.
	os.WriteFile(s.KeyFile(), []byte("not a key"), 0o600)
	if err := s.ensureCert(now); err != nil {
		t.Fatal(err)
	}
	if !certUsable(s.CertFile(), s.KeyFile(), now) {
		t.Error("a damaged key wasn't replaced")
	}
}

// The same site and PHP over HTTPS, with a certificate a browser can be
// told to trust for localhost.
func TestHTTPS(t *testing.T) {
	s := newTestServer(t)
	s.HTTPSPort = freePort(t)
	os.WriteFile(filepath.Join(s.Sites, "index.php"), []byte(`<?php echo "https=", $_SERVER["HTTPS"] ?? "off";`), 0o644)
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Stop(t.Context()) })

	pem, err := os.ReadFile(s.CertFile())
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(pem)
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool},
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort("127.0.0.1", strconv.Itoa(s.HTTPSPort)))
		},
	}}
	resp, err := client.Get("https://localhost:" + strconv.Itoa(s.HTTPSPort) + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(string(body), "https=on") {
		t.Errorf("over HTTPS: %d %s", resp.StatusCode, body)
	}
	if resp.Header.Get("X-Keel-Server") == "" {
		t.Error("the HTTPS site lacks Keel's settings (no X-Keel-Server header)")
	}
	if code, body := get(t, s, "/"); code != 200 || !strings.Contains(body, "https=off") {
		t.Errorf("over HTTP: %d %s", code, body)
	}
}
