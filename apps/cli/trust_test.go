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
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func projectDir(t *testing.T) string {
	t.Helper()
	ws := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(ws, ".blitz"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ws, ".blitz", "settings.toml"), []byte(`
[permissions]
deny = ["web(*)"]

[[hooks.stop]]
command = "true"
`), 0o644))
	return ws
}

func TestTrustCommand(t *testing.T) {
	isolate(t)
	ws := projectDir(t)
	out, err := runCLI(t, "trust", ws)
	require.NoError(t, err)
	for _, s := range []string{"deny rule web(*)", "hook stop runs true", "Not reviewed yet"} {
		assert.Contains(t, out, s)
	}
	out, err = runCLI(t, "trust", "--revoke", ws)
	require.NoError(t, err)
	assert.Contains(t, out, "asked again")
}

// The REPL asks before the workspace opens here, and the answer decides
// what it opens with.
func TestOpenBackendAsksAboutTheProject(t *testing.T) {
	cases := []struct {
		name   string
		o      backendOptions
		loaded bool
		state  string
	}{
		{name: "trusted", o: backendOptions{askTrust: func(api.ProjectSettings) string { return "trust" }}, loaded: true, state: api.TrustTrusted},
		{name: "declined", o: backendOptions{askTrust: func(api.ProjectSettings) string { return "decline" }}, state: api.TrustDeclined},
		{name: "unanswered", o: backendOptions{askTrust: func(api.ProjectSettings) string { return "" }}, state: api.TrustNew},
		{name: "nobody to ask", state: api.TrustNew},
		{name: "--trust-project", o: backendOptions{trustProject: true}, loaded: true, state: api.TrustNew},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			isolate(t)
			ws := projectDir(t)
			cfg, err := loadConfig(&globalFlags{dir: ws})
			require.NoError(t, err)
			c.o.local = true
			w, _, _, err := openBackend(context.Background(), cfg, c.o, func(string) {})
			require.NoError(t, err)
			defer w.Close()
			p := w.ProjectSettings()
			assert.Equal(t, c.loaded, p.Loaded)
			assert.Equal(t, c.state, p.State)
		})
	}
}

// trust with no directory is the current one's; a --dir that isn't there
// is a usage error; and a decision made here is kept.
func TestTrustCommandEdges(t *testing.T) {
	isolate(t)
	ws := projectDir(t)
	t.Chdir(ws)
	out, err := runCLI(t, "trust")
	require.NoError(t, err)
	assert.Contains(t, out, "deny rule web(*)")
	_, err = runCLI(t, "trust", filepath.Join(t.TempDir(), "missing"))
	assert.Equal(t, exitUsage, exitCodeFor(err))

	r, err := client.Attach(context.Background(), os.Getenv("BLITZ_SOCKET"), ws, nil)
	require.NoError(t, err, "trust started the service")
	p := r.ProjectSettings()
	require.NoError(t, r.TrustProject(p.Hash, true))
	assert.Equal(t, api.TrustTrusted, r.ProjectSettings().State)
	out, err = runCLI(t, "trust", "--revoke", ws)
	require.NoError(t, err)
	assert.Contains(t, out, "asked again")
	assert.Equal(t, api.TrustNew, r.ProjectSettings().State, "forgotten in the service")

}
