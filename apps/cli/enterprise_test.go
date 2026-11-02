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
	"strings"
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
	for _, args := range [][]string{{"status"}, {"connect", "https://b.acme.com"}, {"disconnect"}, {"login"}, {"logout"}, {"terms"}, {"workspaces"}, {"open", "x"}, {"create", "x"}} {
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

// runCLIWith is runCLI with input on stdin.
func runCLIWith(t *testing.T, input string, args ...string) (string, error) {
	t.Helper()
	cmd := newRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(input))
	cmd.SetArgs(args)
	err := cmd.Execute()
	stopPrivate()
	return out.String(), err
}

// The organization's terms in the CLI: shown after sign-in when a version
// waits, accepted with y, and declined with anything else, which signs
// out (EN-53).
func TestEnterpriseTerms(t *testing.T) {
	t.Cleanup(http.DefaultClient.CloseIdleConnections)
	isolate(t)
	secrets.SetDefault(&secrets.Memory{})
	idp := enterprisetest.NewProvider(t)
	org := enterprisetest.NewService(t, idp.URL)
	org.Version = "v3"
	_, err := runCLI(t, "enterprise", "connect", org.URL)
	require.NoError(t, err)

	out, err := runCLIWith(t, "n\n", "enterprise", "login")
	assert.Equal(t, exitFailure, exitCodeFor(err), out)
	assert.Contains(t, out, "Acme Assist asks you to accept its terms (version v3):")
	assert.Contains(t, out, "Be kind.")
	assert.Contains(t, out, "Not accepted: signed out of Acme Assist.")

	out, err = runCLIWith(t, "y\n", "enterprise", "login")
	require.NoError(t, err, out)
	assert.Contains(t, out, "✓ Accepted Acme Assist's terms (version v3).")

	out, err = runCLI(t, "enterprise", "terms")
	require.NoError(t, err)
	assert.Contains(t, out, "nothing to accept")

	_, err = runCLI(t, "enterprise", "logout")
	require.NoError(t, err)
	_, err = runCLI(t, "enterprise", "terms")
	assert.Error(t, err, "signed out")
}

// blitz enterprise workspaces and create, and blitz git-credential
// answering Git for the service's host (EN-60, EN-61).
func TestEnterpriseWorkspacesCommand(t *testing.T) {
	t.Cleanup(http.DefaultClient.CloseIdleConnections)
	isolate(t)
	secrets.SetDefault(&secrets.Memory{})
	idp := enterprisetest.NewProvider(t)
	org := enterprisetest.NewService(t, idp.URL)
	_, err := runCLI(t, "enterprise", "connect", org.URL)
	require.NoError(t, err)
	_, err = runCLI(t, "enterprise", "login")
	require.NoError(t, err)

	out, err := runCLI(t, "enterprise", "workspaces")
	require.NoError(t, err)
	assert.Contains(t, out, "You have no workspaces in Acme Assist yet")
	out, err = runCLI(t, "enterprise", "create", "Q4 Launch")
	require.NoError(t, err)
	assert.Contains(t, out, "✓ Made Q4 Launch (q4-launch). Open it: blitz enterprise open q4-launch")
	out, err = runCLI(t, "enterprise", "workspaces")
	require.NoError(t, err)
	assert.Contains(t, out, "q4-launch")
	assert.Contains(t, out, "not on this computer: blitz enterprise open q4-launch")
	_, err = runCLI(t, "enterprise", "open", "nope")
	assert.Error(t, err)
	// One already on this computer: listed where it is, and opened there.
	home, _ := os.UserHomeDir()
	clone := filepath.Join(home, "Blitz", "Acme Assist", "q4-launch")
	require.NoError(t, os.MkdirAll(filepath.Join(clone, ".git"), 0o700))
	out, err = runCLI(t, "enterprise", "workspaces")
	require.NoError(t, err)
	assert.Contains(t, out, clone)
	out, err = runCLI(t, "enterprise", "open", "q4-launch")
	require.NoError(t, err)
	assert.Equal(t, clone+"\n", out)

	host := strings.TrimPrefix(org.URL, "http://")
	out, err = runCLIWith(t, "protocol=http\nhost="+host+"\n\n", "git-credential", "get")
	require.NoError(t, err)
	assert.Contains(t, out, "username=blitz\npassword=")
	out, err = runCLIWith(t, "protocol=https\nhost=github.com\n\n", "git-credential", "get")
	require.NoError(t, err)
	assert.Empty(t, out)

	org.Close()
	for _, args := range [][]string{{"workspaces"}, {"create", "y"}} {
		_, err := runCLI(t, append([]string{"enterprise"}, args...)...)
		assert.Error(t, err, args)
	}
}
