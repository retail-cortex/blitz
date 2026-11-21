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
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/enterprise/enterprisetest"
	"github.com/retail-cortex/blitz/pkg/secrets"
)

// PK-12 to PK-16: blitz pkg checks, compiles and lists the workspace's
// assets, adds from a folder and from the registry with a token, verifies
// them, searches and publishes, by a token or the person's sign-in.
func TestPkgCommand(t *testing.T) {
	t.Cleanup(http.DefaultClient.CloseIdleConnections)
	isolate(t)
	secrets.SetDefault(&secrets.Memory{})
	ws, src := t.TempDir(), t.TempDir()
	put := func(root, rel, text string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(text), 0o644))
	}
	put(ws, ".agents/skills/tone/SKILL.md", "---\nname: tone\ndescription: Our tone\n---\nBe kind.\n")
	put(src, "notes/SKILL.md", "---\nname: notes\n---\nTake notes. See [this](gone.md).\n")

	out, err := runCLI(t, "-d", t.TempDir(), "pkg", "list")
	require.NoError(t, err)
	assert.Contains(t, out, "no agents, skills")
	_, err = runCLI(t, "pkg", "list")
	assert.NoError(t, err, "here")
	out, err = runCLI(t, "-d", ws, "pkg", "validate")
	require.NoError(t, err)
	assert.Contains(t, out, "Every asset reads")
	out, err = runCLI(t, "-d", ws, "pkg", "list")
	require.NoError(t, err)
	assert.Contains(t, out, "skill/tone · the workspace's own")
	out, err = runCLI(t, "-d", ws, "pkg", "verify")
	require.NoError(t, err)
	assert.Contains(t, out, "no packages")

	out, err = runCLI(t, "-d", ws, "pkg", "add", "file://"+filepath.Join(src, "notes"))
	require.NoError(t, err)
	assert.Contains(t, out, "Added skill/notes from file://")
	out, err = runCLI(t, "-d", ws, "pkg", "list")
	require.NoError(t, err)
	assert.Contains(t, out, "skill/notes · file://")
	assert.Contains(t, out, "skill/tone · the workspace's own")
	out, err = runCLI(t, "-d", ws, "pkg", "validate")
	require.NoError(t, err)
	assert.Contains(t, out, "⚠ skill/notes: it has no description")
	out, err = runCLI(t, "-d", ws, "pkg", "validate", "--json")
	require.NoError(t, err)
	assert.Contains(t, out, `"asset": "skill/notes"`)
	out, err = runCLI(t, "-d", ws, "pkg", "verify")
	require.NoError(t, err)
	assert.Contains(t, out, "✓ skill/notes · as added")
	put(ws, ".agents/skills/notes/SKILL.md", "changed")
	out, err = runCLI(t, "-d", ws, "pkg", "verify")
	assert.Equal(t, exitFailure, exitCodeFor(err), "%v\n%s", err, out)
	assert.Contains(t, out, "✗ skill/notes · changed since it was added")
	out, err = runCLI(t, "-d", ws, "pkg", "validate")
	assert.Equal(t, exitFailure, exitCodeFor(err), "a SKILL.md with no front matter: %s", out)

	out, err = runCLI(t, "-d", ws, "pkg", "compile", "--castor")
	require.NoError(t, err)
	assert.Contains(t, out, "agents_manifest.json")
	assert.Contains(t, out, "skills_manifest.json")

	// The registry: none yet.
	_, err = runCLI(t, "-d", ws, "pkg", "search", "tone")
	assert.ErrorContains(t, err, "No registry")
	_, err = runCLI(t, "-d", ws, "pkg", "publish", "skill/tone")
	assert.ErrorContains(t, err, "No registry")
	_, err = runCLI(t, "-d", ws, "pkg", "publish", "tone")
	assert.Equal(t, exitUsage, exitCodeFor(err))
	_, err = runCLI(t, "-d", ws, "pkg", "publish", "skill/nope")
	assert.Error(t, err)

	idp := enterprisetest.NewProvider(t)
	org := enterprisetest.NewService(t, idp.URL)
	org.RegistryToken = "blitzreg_ci"
	org.Registry = map[string]enterprisetest.RegistryAsset{"skill/sql-review": {Version: "1.4.2", Description: "Reviews SQL", Tags: []string{"sql"}, Lifecycle: "deprecated", Files: map[string]string{"skills/sql-review/SKILL.md": "---\nname: sql-review\ndescription: Reviews SQL\n---\nReview.\n"}}}
	t.Setenv("BLITZ_REGISTRY_TOKEN", "blitzreg_ci")
	_, err = runCLI(t, "-d", ws, "pkg", "search")
	assert.ErrorContains(t, err, "--registry")
	out, err = runCLI(t, "-d", ws, "pkg", "search", "sql", "--registry", org.URL)
	require.NoError(t, err)
	assert.Contains(t, out, "skill/sql-review 1.4.2  Reviews SQL  (deprecated) #sql")
	out, err = runCLI(t, "-d", ws, "pkg", "search", "nothing-like-it", "--registry", org.URL)
	require.NoError(t, err)
	assert.Contains(t, out, "Nothing in your organization's registry matches")
	host := strings.TrimPrefix(org.URL, "http://")
	out, err = runCLI(t, "-d", ws, "pkg", "add", "blitz://"+host+"/skill/sql-review@1.x", "--registry", org.URL)
	require.NoError(t, err)
	assert.Contains(t, out, "Added skill/sql-review from blitz://"+host+"/skill/sql-review@1.x · 1.4.2")
	out, err = runCLI(t, "-d", ws, "pkg", "list")
	require.NoError(t, err)
	assert.Contains(t, out, "skill/sql-review · blitz://"+host+"/skill/sql-review@1.x · 1.4.2")
	out, err = runCLI(t, "-d", ws, "pkg", "publish", "skill/tone", "--bump", "minor", "--note", "from CI", "--registry", org.URL)
	require.NoError(t, err)
	assert.Contains(t, out, "Published skill/tone 1.0.0")
	put(ws, ".agents/skills/tested/SKILL.md", "---\nname: tested\ndescription: Tested\n---\nT.\n")
	out, err = runCLI(t, "-d", ws, "pkg", "publish", "skill/tested", "--registry", org.URL)
	require.NoError(t, err)
	assert.Contains(t, out, "its evals are running")
	_, err = runCLI(t, "-d", ws, "pkg", "add", "blitz://"+host+"/skill/nope", "--registry", org.URL)
	assert.Error(t, err, "not in the registry")
	org.Mu.Lock()
	require.Len(t, org.Published, 2)
	assert.Equal(t, "minor", org.Published[0]["bump"])
	org.Mu.Unlock()

	// Signed in, without a token.
	t.Setenv("BLITZ_REGISTRY_TOKEN", "")
	_, err = runCLI(t, "enterprise", "connect", org.URL)
	require.NoError(t, err)
	_, err = runCLI(t, "-d", ws, "pkg", "search", "review")
	assert.ErrorContains(t, err, "No registry", "connected, not signed in")
	_, err = runCLI(t, "enterprise", "login")
	require.NoError(t, err)
	out, err = runCLI(t, "-d", ws, "pkg", "search", "review")
	require.NoError(t, err)
	assert.Contains(t, out, "skill/sql-review")
}

// blitz pkg's registry: a settings file that doesn't read, a registry
// address that doesn't; a hook outside a repository.
func TestPkgRegistryErrors(t *testing.T) {
	home := isolate(t)
	t.Setenv("BLITZ_REGISTRY_TOKEN", "blitzreg_ci")
	_, err := runCLI(t, "pkg", "search", "--registry", "::not a url")
	assert.Error(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".blitz"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".blitz", ".env.toml"), []byte("[[[ not toml"), 0o600))
	for _, args := range [][]string{{"search"}, {"validate"}, {"add", "blitz://blitz.acme.com/skill/x"}} {
		_, err = runCLI(t, append([]string{"pkg"}, args...)...)
		assert.Error(t, err, "a broken settings file: %v", args)
	}
	ws := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(ws, ".agents"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ws, ".agents", "blitz.lock"), []byte("{nope"), 0o644))
	_, err = runCLI(t, "-d", ws, "pkg", "verify")
	assert.Error(t, err, "a lockfile that doesn't read")
	_, err = runCLI(t, "-d", ws, "pkg", "list")
	assert.Error(t, err, "list with corrupt lockfile")
	_, err = runCLI(t, "-d", t.TempDir(), "hook", "run", "pre-commit")
	assert.Error(t, err, "not a repository")

	// Publish & search with unreachable/invalid registry and validate with service down.
	ws2 := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(ws2, ".agents", "skills", "tone"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ws2, ".agents", "skills", "tone", "SKILL.md"), []byte("---\nname: tone\ndescription: Tone\n---\nHi.\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".blitz", ".env.toml"), []byte(""), 0o600))
	_, err = runCLI(t, "-d", ws2, "pkg", "publish", "skill/tone", "--registry", "::not a url")
	assert.Error(t, err)
	_, err = runCLI(t, "-d", ws2, "pkg", "search", "tone", "--registry", "http://127.0.0.1:1")
	assert.Error(t, err)
	_, err = runCLI(t, "-d", ws2, "pkg", "publish", "skill/tone", "--registry", "http://127.0.0.1:1")
	assert.Error(t, err)

	inner := startShared
	startShared = func(context.Context, string) error { return errors.New("service down") }
	t.Cleanup(func() { startShared = inner })
	_, err = runCLI(t, "-d", ws2, "pkg", "validate")
	assert.ErrorContains(t, err, "service down")
}
