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

package netutil

import (
	"crypto/tls"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeCertPEM(t *testing.T, srv *httptest.Server, name string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	data := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	require.NoError(t, os.WriteFile(p, data, 0o600))
	return p
}

func TestLoadCertPool(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	valid := writeCertPEM(t, srv, "ca.pem")
	empty := filepath.Join(t.TempDir(), "empty.pem")
	require.NoError(t, os.WriteFile(empty, nil, 0o600))
	bad := filepath.Join(t.TempDir(), "bad.pem")
	require.NoError(t, os.WriteFile(bad, []byte("not a pem cert"), 0o600))
	missing := filepath.Join(t.TempDir(), "missing.pem")

	tests := []struct {
		name    string
		path    string
		wantErr string
	}{
		{name: "valid PEM", path: valid},
		{name: "missing file", path: missing, wantErr: "[network] ca_file:"},
		{name: "empty file", path: empty, wantErr: "no PEM certificates in it"},
		{name: "invalid PEM", path: bad, wantErr: "no PEM certificates in it"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool, err := LoadCertPool(tt.path)
			if tt.wantErr != "" {
				assert.ErrorContains(t, err, tt.wantErr)
				assert.Nil(t, pool)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, pool)
		})
	}
}

func TestExtraCAsAndClients(t *testing.T) {
	t.Cleanup(ResetExtraCAs)
	ResetExtraCAs()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "trusted")
	}))
	defer srv.Close()

	get := func(c *http.Client) (string, error) {
		defer c.CloseIdleConnections()
		resp, err := c.Get(srv.URL)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		return string(b), err
	}

	assert.Nil(t, TLSConfig())
	_, err := get(NewClient(5 * time.Second))
	assert.Error(t, err, "untrusted before SetExtraCAs")

	var beforeTLS *tls.Config
	if dt, ok := http.DefaultTransport.(*http.Transport); ok {
		beforeTLS = dt.TLSClientConfig
	}

	ca := writeCertPEM(t, srv, "ca.pem")
	require.NoError(t, SetExtraCAs(ca))

	// DefaultTransport itself must never be mutated in place.
	if dt, ok := http.DefaultTransport.(*http.Transport); ok {
		assert.Equal(t, beforeTLS, dt.TLSClientConfig, "http.DefaultTransport must not be mutated")
		if dt.TLSClientConfig != nil {
			assert.Nil(t, dt.TLSClientConfig.RootCAs, "http.DefaultTransport RootCAs must remain nil")
		}
	}

	tc := TLSConfig()
	require.NotNil(t, tc)
	assert.Equal(t, uint16(tls.VersionTLS12), tc.MinVersion)

	c := NewClient(5 * time.Second)
	assert.Equal(t, 5*time.Second, c.Timeout)
	body, err := get(c)
	require.NoError(t, err)
	assert.Equal(t, "trusted", body)

	// Mutating one caller's TLSConfig does not affect subsequent clones.
	tc.NextProtos = []string{"h2"}
	assert.Empty(t, TLSConfig().NextProtos)

	// Clearing with empty string resets extra CAs.
	require.NoError(t, SetExtraCAs(""))
	assert.Nil(t, TLSConfig())
	_, err = get(NewClient(5 * time.Second))
	assert.Error(t, err, "untrusted after clearing extra CAs")
}

func TestNewTransport(t *testing.T) {
	t.Cleanup(ResetExtraCAs)
	ResetExtraCAs()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "via transport")
	}))
	defer srv.Close()

	ca := writeCertPEM(t, srv, "ca.pem")
	tr, err := NewTransport(ca)
	require.NoError(t, err)
	c := &http.Client{Timeout: 5 * time.Second, Transport: tr}
	defer c.CloseIdleConnections()
	resp, err := c.Get(srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "via transport", string(b))

	trEmpty, err := NewTransport("")
	require.NoError(t, err)
	if trEmpty.TLSClientConfig != nil {
		assert.Nil(t, trEmpty.TLSClientConfig.RootCAs)
	}

	_, err = NewTransport(filepath.Join(t.TempDir(), "missing.pem"))
	assert.ErrorContains(t, err, "[network] ca_file:")

	// Error propagation from SetExtraCAs on a missing file.
	assert.ErrorContains(t, SetExtraCAs(filepath.Join(t.TempDir(), "missing.pem")), "[network] ca_file:")
}

func TestCloneAndNewTransportDefaultVariants(t *testing.T) {
	t.Cleanup(ResetExtraCAs)
	ResetExtraCAs()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()
	ca := writeCertPEM(t, srv, "ca.pem")

	orig := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = orig })

	t.Run("non-http.Transport default", func(t *testing.T) {
		http.DefaultTransport = nil
		tr := CloneDefaultTransport()
		require.NotNil(t, tr)
		assert.Nil(t, tr.TLSClientConfig)

		// NewTransport with nil TLSClientConfig creates a fresh TLS 1.2 config.
		trWithCA, err := NewTransport(ca)
		require.NoError(t, err)
		require.NotNil(t, trWithCA.TLSClientConfig)
		assert.Equal(t, uint16(tls.VersionTLS12), trWithCA.TLSClientConfig.MinVersion)
		assert.NotNil(t, trWithCA.TLSClientConfig.RootCAs)

		// CloneDefaultTransport with extra CAs attaches TLSConfig directly when tr.TLSClientConfig is nil.
		require.NoError(t, SetExtraCAs(ca))
		trExtra := CloneDefaultTransport()
		require.NotNil(t, trExtra.TLSClientConfig)
		assert.NotNil(t, trExtra.TLSClientConfig.RootCAs)
		ResetExtraCAs()
	})

	t.Run("default transport with existing TLSClientConfig", func(t *testing.T) {
		base := &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS10},
		}
		http.DefaultTransport = base
		require.NoError(t, SetExtraCAs(ca))
		tr := CloneDefaultTransport()
		require.NotNil(t, tr.TLSClientConfig)
		assert.Equal(t, uint16(tls.VersionTLS12), tr.TLSClientConfig.MinVersion)
		assert.NotNil(t, tr.TLSClientConfig.RootCAs)
		assert.Equal(t, uint16(tls.VersionTLS10), base.TLSClientConfig.MinVersion, "base transport not mutated")
		assert.Nil(t, base.TLSClientConfig.RootCAs, "base RootCAs not mutated")
		ResetExtraCAs()
	})
}

func TestConcurrentSetExtraCAsAndClone(t *testing.T) {
	t.Cleanup(ResetExtraCAs)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()
	ca := writeCertPEM(t, srv, "ca.pem")

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := range 50 {
				if (n+j)%2 == 0 {
					assert.NoError(t, SetExtraCAs(ca))
				} else {
					ResetExtraCAs()
				}
				tr := CloneDefaultTransport()
				if tr.TLSClientConfig != nil {
					tr.TLSClientConfig.NextProtos = []string{"http/1.1"}
				}
				_ = NewClient(time.Second)
			}
		}(i)
	}
	wg.Wait()
}
