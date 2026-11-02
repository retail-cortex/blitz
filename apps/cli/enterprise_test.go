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

package main

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/client"
	"github.com/retail-cortex/blitz/pkg/enterprise/enterprisetest"
	"github.com/retail-cortex/blitz/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// blitz enterprise: connect, sign in with a code, the status, sign out,
// disconnect; an address that isn't a service is a usage error.
func TestEnterpriseCommand(t *testing.T) {
	t.Cleanup(http.DefaultClient.CloseIdleConnections)
	home := isolate(t)
	secrets.SetDefault(&secrets.Memory{})
	idp := enterprisetest.NewProvider(t)
	org := enterprisetest.NewService(t, idp.URL)

	out, err := runCLI(t, "enterprise", "status")
	require.NoError(t, err)
	assert.Contains(t, out, "Not connected to an organization")
	_, err = runCLI(t, "enterprise", "login")
	assert.ErrorContains(t, err, "connect to your organization's service first")

	_, err = runCLI(t, "enterprise", "connect", "http://blitz.acme.com")
	assert.Equal(t, exitUsage, exitCodeFor(err), "%v", err)
	out, err = runCLI(t, "enterprise", "connect", org.URL)
	require.NoError(t, err)
	assert.Contains(t, out, "✓ Connected to Acme Assist ("+org.URL+")")

	out, err = runCLI(t, "enterprise", "status")
	require.NoError(t, err)
	assert.Contains(t, out, "version 1.0.0; offers gateway, jobs, branding")
	assert.Contains(t, out, "Signed in     no: blitz enterprise login")

	out, err = runCLI(t, "enterprise", "login")
	require.NoError(t, err, out)
	assert.Contains(t, out, "Open https://www.google.com/device and enter the code ABCD-EFGH")
	assert.Contains(t, out, "✓ Signed in to Acme Assist as ana@acme.com")

	out, err = runCLI(t, "enterprise", "status")
	require.NoError(t, err)
	assert.Contains(t, out, "Signed in     Ana <ana@acme.com> (member)")
	assert.Contains(t, out, "Renews")

	org.Terms = "2026-11"
	idp.IDToken = "for-someone-else"
	_, err = runCLI(t, "enterprise", "login")
	assert.Equal(t, exitFailure, exitCodeFor(err))
	idp.IDToken = ""

	out, err = runCLI(t, "enterprise", "logout")
	require.NoError(t, err)
	assert.Contains(t, out, "Signed out of Acme Assist.")
	out, err = runCLI(t, "enterprise", "disconnect")
	require.NoError(t, err)
	assert.Contains(t, out, "Blitz uses your own model providers again")
	out, err = runCLI(t, "enterprise", "logout")
	require.NoError(t, err)
	assert.Contains(t, out, "Not connected to an organization.")

	// Settings the service can't use: each command says so.
	require.NoError(t, os.WriteFile(filepath.Join(home, ".blitz", ".env.toml"), []byte("[enterprise]\nurl = \"http://evil.example\"\n"), 0o600))
	for _, args := range [][]string{{"status"}, {"login"}, {"logout"}} {
		_, err := runCLI(t, append([]string{"enterprise"}, args...)...)
		assert.Error(t, err, args)
	}
	_, err = runCLI(t, "enterprise", "disconnect")
	assert.NoError(t, err, "disconnecting is how a bad address is undone")

	// No service: each command says it couldn't start one.
	file := filepath.Join(t.TempDir(), "a-file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	t.Setenv("BLITZ_SOCKET", filepath.Join(file, "s.sock"))
	for _, args := range [][]string{{"status"}, {"connect", "https://b.acme.com"}, {"disconnect"}, {"login"}, {"logout"}} {
		_, err := runCLI(t, append([]string{"enterprise"}, args...)...)
		assert.ErrorContains(t, err, "starting the Blitz service", args)
	}
}

// The status says what it knows: a service not reached, the terms to
// accept, a sign-in without a name or roles.
func TestDescribeOrganization(t *testing.T) {
	cases := map[string]struct {
		o    client.Organization
		want []string
	}{
		"not reached": {client.Organization{URL: "https://b.acme.com", Checked: true, Error: "reaching the organization's service: refused"},
			[]string{"Organization  https://b.acme.com (https://b.acme.com)", "Service       not reached: reaching"}},
		"terms": {client.Organization{URL: "https://b.acme.com", Name: "Acme", SignedIn: true, Email: "ana@acme.com", Terms: "v2", Renews: time.Now()},
			[]string{"Signed in     ana@acme.com\n", "Terms         version v2 to accept"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			var b bytes.Buffer
			describeOrganization(&b, c.o)
			for _, w := range c.want {
				assert.Contains(t, b.String(), w)
			}
		})
	}
}
