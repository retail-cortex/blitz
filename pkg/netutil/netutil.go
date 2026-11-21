// Copyright 2026 Retail Cortex
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package netutil provides race-free CA bundle loading, HTTP transport
// cloning, and bounded HTTP client construction without mutating
// http.DefaultTransport in place.
package netutil

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"
)

var (
	extraCAsMu sync.RWMutex
	extraCAs   *x509.CertPool
)

// LoadCertPool returns a copy of the system certificate pool augmented with
// the PEM-encoded certificates read from caFile.
func LoadCertPool(caFile string) (*x509.CertPool, error) {
	pem, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("[network] ca_file: %w", err)
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("[network] ca_file %s: no PEM certificates in it", caFile)
	}
	return pool, nil
}

// SetExtraCAs loads PEM certificates from caFile on top of the system
// certificate pool for subsequent TLSConfig, CloneDefaultTransport, and
// NewClient calls. Passing "" clears any previously configured extra CAs.
// It never mutates http.DefaultTransport in place.
func SetExtraCAs(caFile string) error {
	if caFile == "" {
		ResetExtraCAs()
		return nil
	}
	pool, err := LoadCertPool(caFile)
	if err != nil {
		return err
	}
	extraCAsMu.Lock()
	extraCAs = pool
	extraCAsMu.Unlock()
	return nil
}

// ResetExtraCAs clears any extra certificate authorities configured via
// SetExtraCAs.
func ResetExtraCAs() {
	extraCAsMu.Lock()
	extraCAs = nil
	extraCAsMu.Unlock()
}

// TLSConfig returns a fresh TLS configuration trusting the extra CAs set via
// SetExtraCAs (with MinVersion TLS 1.2), or nil when none are configured.
func TLSConfig() *tls.Config {
	extraCAsMu.RLock()
	pool := extraCAs
	extraCAsMu.RUnlock()
	if pool == nil {
		return nil
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    pool,
	}
}

// CloneDefaultTransport clones Go's default transport and attaches TLSConfig
// if extra CAs are configured, without ever mutating http.DefaultTransport
// or sharing mutable *tls.Config pointers across callers.
func CloneDefaultTransport() *http.Transport {
	var tr *http.Transport
	if base, ok := http.DefaultTransport.(*http.Transport); ok && base != nil {
		tr = base.Clone()
		if tr.TLSClientConfig != nil {
			tr.TLSClientConfig = tr.TLSClientConfig.Clone()
		}
	} else {
		tr = &http.Transport{
			Proxy: http.ProxyFromEnvironment,
		}
	}
	if tc := TLSConfig(); tc != nil {
		if tr.TLSClientConfig != nil {
			c := tr.TLSClientConfig.Clone()
			c.RootCAs = tc.RootCAs
			if c.MinVersion < tls.VersionTLS12 {
				c.MinVersion = tls.VersionTLS12
			}
			tr.TLSClientConfig = c
		} else {
			tr.TLSClientConfig = tc
		}
	}
	return tr
}

// NewTransport clones Go's default transport and, when caFile is non-empty,
// configures RootCAs from LoadCertPool(caFile).
func NewTransport(caFile string) (*http.Transport, error) {
	tr := CloneDefaultTransport()
	if caFile == "" {
		return tr, nil
	}
	pool, err := LoadCertPool(caFile)
	if err != nil {
		return nil, err
	}
	if tr.TLSClientConfig != nil {
		tr.TLSClientConfig = tr.TLSClientConfig.Clone()
	} else {
		tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	tr.TLSClientConfig.RootCAs = pool
	return tr, nil
}

// NewClient returns an HTTP client bounded by timeout using a cloned default
// transport (including any extra CAs configured via SetExtraCAs).
func NewClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: CloneDefaultTransport(),
	}
}
