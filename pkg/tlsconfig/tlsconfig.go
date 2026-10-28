// Package tlsconfig provides the shared mTLS command-line flags and the
// client tls.Config built from them. Server certificates are verified against
// the system trust store; on Linux/Unix a private CA can be supplied with the
// standard SSL_CERT_FILE or SSL_CERT_DIR environment variables.
package tlsconfig

import (
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
)

// Flags holds the paths of the client certificate and key used for mTLS.
type Flags struct {
	CertFile string
	KeyFile  string
}

// Register binds the mTLS flags to the given FlagSet.
func (f *Flags) Register(fs *flag.FlagSet) {
	fs.StringVar(&f.CertFile, "client-cert", "", "Path to a PEM client certificate for mTLS (requires --client-key and an https URL)")
	fs.StringVar(&f.KeyFile, "client-key", "", "Path to the PEM private key for --client-cert")
}

// Enabled reports whether any mTLS flag was set.
func (f *Flags) Enabled() bool {
	return f.CertFile != "" || f.KeyFile != ""
}

// CheckScheme returns an error if mTLS is enabled for a non-https target,
// which would otherwise silently send plaintext.
func (f *Flags) CheckScheme(scheme string) error {
	if f.Enabled() && scheme != "https" {
		return fmt.Errorf("mTLS flags require the https scheme, got %q", scheme)
	}
	return nil
}

// Load builds a client tls.Config presenting the configured certificate.
// It returns nil when no mTLS flag was set, leaving the transport defaults untouched.
func (f *Flags) Load() (*tls.Config, error) {
	if !f.Enabled() {
		return nil, nil
	}
	if f.CertFile == "" || f.KeyFile == "" {
		return nil, errors.New("--client-cert and --client-key must be provided together")
	}
	cert, err := tls.LoadX509KeyPair(f.CertFile, f.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("loading client certificate: %w", err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}, nil
}
