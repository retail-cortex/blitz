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

package engine

import (
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/netutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// [network] ca_file: clients built via netutil trust it without mutating
// http.DefaultTransport in place.
func TestSetupNetwork(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "trusted") }))
	defer srv.Close()
	t.Cleanup(netutil.ResetExtraCAs)
	get := func() error {
		c := &http.Client{Transport: netutil.CloneDefaultTransport()}
		defer c.CloseIdleConnections()
		resp, err := c.Get(srv.URL)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		assert.Equal(t, "trusted", string(body))
		return nil
	}
	assert.Error(t, get(), "an unknown authority, before")

	beforeTLS := http.DefaultTransport.(*http.Transport).TLSClientConfig
	ca := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o644))
	cfg := config.DefaultConfig()
	require.NoError(t, SetupNetwork(cfg), "nothing to do")
	cfg.Network.CAFile = ca
	require.NoError(t, SetupNetwork(cfg))
	assert.Equal(t, beforeTLS, http.DefaultTransport.(*http.Transport).TLSClientConfig, "http.DefaultTransport is never mutated")
	assert.NoError(t, get())
	// Another workspace opening without ca_file keeps the authorities.
	require.NoError(t, SetupNetwork(config.DefaultConfig()))
	assert.NoError(t, get(), "a workspace without ca_file took them away")

	cfg.Network.CAFile = filepath.Join(t.TempDir(), "none.pem")
	assert.ErrorContains(t, SetupNetwork(cfg), "ca_file")
	bad := filepath.Join(t.TempDir(), "bad.pem")
	require.NoError(t, os.WriteFile(bad, []byte("not a certificate"), 0o644))
	cfg.Network.CAFile = bad
	assert.ErrorContains(t, SetupNetwork(cfg), "no PEM certificates")
}
