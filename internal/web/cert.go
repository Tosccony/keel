package web

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// certLife is how long Keel's HTTPS certificate lasts; it's made again
// when less than certRenew is left.
const (
	certLife  = 2 * 365 * 24 * time.Hour
	certRenew = 30 * 24 * time.Hour
)

// CertFile and KeyFile are the HTTPS certificate Keel makes for this
// computer, and its private key.
func (s *Server) CertFile() string { return filepath.Join(s.conf(), "ssl", "localhost.crt") }
func (s *Server) KeyFile() string  { return filepath.Join(s.conf(), "ssl", "localhost.key") }

// ensureCert makes a self-signed certificate for localhost, 127.0.0.1 and
// ::1, unless a usable one is already there. Like XAMPP's, browsers warn
// about it once; it only ever serves this computer.
func (s *Server) ensureCert(now time.Time) error {
	if certUsable(s.CertFile(), s.KeyFile(), now) {
		return nil
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("web: HTTPS key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return fmt.Errorf("web: HTTPS certificate: %w", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "localhost", Organization: []string{"Keel (this computer only)"}},
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(certLife),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("web: HTTPS certificate: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return fmt.Errorf("web: HTTPS key: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.CertFile()), 0o755); err != nil {
		return fmt.Errorf("web: %w", err)
	}
	if err := writeAtomic(s.KeyFile(), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return fmt.Errorf("web: HTTPS key: %w", err)
	}
	if err := writeAtomic(s.CertFile(), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return fmt.Errorf("web: HTTPS certificate: %w", err)
	}
	return nil
}

// certUsable reports whether the certificate and key load as a pair and
// the certificate lasts past the renewal window.
func certUsable(certFile, keyFile string, now time.Time) bool {
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil || len(pair.Certificate) == 0 {
		return false
	}
	cert, err := x509.ParseCertificate(pair.Certificate[0])
	return err == nil && now.Add(certRenew).Before(cert.NotAfter)
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
