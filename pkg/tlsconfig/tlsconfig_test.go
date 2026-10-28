package tlsconfig

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newCert issues a certificate for template, signed by parent (self-signed when parent is nil).
func newCert(t *testing.T, template *x509.Certificate, parent *x509.Certificate, parentKey *ecdsa.PrivateKey) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template.NotBefore = time.Now().Add(-time.Hour)
	template.NotAfter = time.Now().Add(time.Hour)
	if parent == nil {
		parent, parentKey = template, key
	}
	der, err := x509.CreateCertificate(rand.Reader, template, parent, &key.PublicKey, parentKey)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert, key
}

func writePEM(t *testing.T, dir, name, blockType string, der []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadDisabledReturnsNil(t *testing.T) {
	cfg, err := (&Flags{}).Load()
	if err != nil || cfg != nil {
		t.Fatalf("Load() = %v, %v; want nil, nil", cfg, err)
	}
}

func TestLoadRequiresCertAndKeyTogether(t *testing.T) {
	for _, f := range []Flags{{CertFile: "client.pem"}, {KeyFile: "client.key"}} {
		if _, err := f.Load(); err == nil {
			t.Fatalf("Load(%+v) succeeded; want error", f)
		}
	}
}

func TestLoadMissingFiles(t *testing.T) {
	f := Flags{CertFile: "missing.pem", KeyFile: "missing.key"}
	if _, err := f.Load(); err == nil {
		t.Fatal("Load() with missing files succeeded; want error")
	}
}

func TestCheckScheme(t *testing.T) {
	enabled := Flags{CertFile: "c", KeyFile: "k"}
	if err := enabled.CheckScheme("http"); err == nil {
		t.Fatal("CheckScheme(http) with mTLS succeeded; want error")
	}
	if err := enabled.CheckScheme("https"); err != nil {
		t.Fatalf("CheckScheme(https) = %v; want nil", err)
	}
	if err := (&Flags{}).CheckScheme("http"); err != nil {
		t.Fatalf("CheckScheme(http) without mTLS = %v; want nil", err)
	}
}

func TestLoadAuthenticatesToMTLSServer(t *testing.T) {
	ca, caKey := newCert(t, &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test ca"},
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}, nil, nil)
	serverCert, serverKey := newCert(t, &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "localhost"},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}, ca, caKey)
	clientCert, clientKey := newCert(t, &x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject:      pkix.Name{CommonName: "client"},
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}, ca, caKey)

	pool := x509.NewCertPool()
	pool.AddCert(ca)

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.TLS = &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{serverCert.Raw}, PrivateKey: serverKey}},
		ClientCAs:    pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}
	srv.StartTLS()
	defer srv.Close()

	dir := t.TempDir()
	keyDER, err := x509.MarshalECPrivateKey(clientKey)
	if err != nil {
		t.Fatal(err)
	}
	f := Flags{
		CertFile: writePEM(t, dir, "client.pem", "CERTIFICATE", clientCert.Raw),
		KeyFile:  writePEM(t, dir, "client.key", "EC PRIVATE KEY", keyDER),
	}

	get := func(cfg *tls.Config) error {
		if cfg == nil {
			cfg = &tls.Config{}
		}
		cfg.RootCAs = pool // trust the test CA without touching the system store
		client := &http.Client{Transport: &http.Transport{TLSClientConfig: cfg}}
		resp, err := client.Get(srv.URL)
		if err != nil {
			return err
		}
		return resp.Body.Close()
	}

	cfg, err := f.Load()
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if err := get(cfg); err != nil {
		t.Fatalf("request with client certificate failed: %v", err)
	}
	if err := get(nil); err == nil {
		t.Fatal("request without client certificate succeeded; want handshake failure")
	}
}
