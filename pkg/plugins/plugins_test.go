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

package plugins

import (
	"archive/zip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// files writes a tree of files under dir.
func files(t *testing.T, dir string, tree map[string]string) string {
	t.Helper()
	for p, content := range tree {
		full := filepath.Join(dir, filepath.FromSlash(p))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}
	return dir
}

// sample is a plugin with one of everything.
func sample(t *testing.T) string {
	return files(t, filepath.Join(t.TempDir(), "lint-kit"), map[string]string{
		"plugin.toml":          "name = \"lint-kit\"\nversion = \"1.2.0\"\ndescription = \"Linting helpers\"\n",
		"skills/lint/SKILL.md": "---\nname: lint\ndescription: Lint the code\n---\nRun the linter.\n",
		"commands/fix-lint.md": "---\ndescription: Fix lint errors\n---\nFix every lint error.\n",
		"agents/linter.md":     "---\nname: linter\ndescription: Lints\ntools: [read_file, grep]\n---\nYou lint.\n",
		"hooks.toml":           "[[post_tool]]\nmatch = \"create_file\"\ncommand = \"${BLITZ_PLUGIN_ROOT}/bin/check.sh\"\n",
		"mcp.toml":             "[[servers]]\nname = \"lintd\"\ncommand = \"${BLITZ_PLUGIN_ROOT}/bin/lintd\"\nargs = [\"--root\", \"${BLITZ_PLUGIN_ROOT}\"]\n",
		"bin/check.sh":         "#!/bin/sh\n",
		".git/HEAD":            "ref: refs/heads/main\n",
	})
}

func TestRead(t *testing.T) {
	p, err := Read(sample(t))
	require.NoError(t, err)
	assert.Equal(t, "lint-kit", p.Name)
	assert.Equal(t, []string{"lint"}, p.Skills)
	assert.Equal(t, []string{"fix-lint"}, p.Commands)
	assert.Equal(t, []string{"linter"}, p.Agents)
	assert.True(t, p.RunsCode())
	assert.Equal(t, []string{
		"runs code: post_tool hook: ${BLITZ_PLUGIN_ROOT}/bin/check.sh",
		"MCP server lintd: runs code: ${BLITZ_PLUGIN_ROOT}/bin/lintd --root ${BLITZ_PLUGIN_ROOT}",
		"skill: lint (its scripts run code when used)",
		"command: /fix-lint",
		"agent: linter",
	}, p.Summary())

	for _, tt := range []struct {
		name    string
		tree    map[string]string
		wantErr string
	}{
		{"no manifest", map[string]string{"x": ""}, "not a plugin"},
		{"bad name", map[string]string{"plugin.toml": "name = \"Bad Name\""}, "name"},
		{"bad hooks", map[string]string{"plugin.toml": "name = \"a\"", "hooks.toml": "[[pre_tool]\n"}, "hooks.toml"},
		{"server without a command", map[string]string{"plugin.toml": "name = \"a\"", "mcp.toml": "[[servers]]\nname = \"x\"\n"}, "one of command or url"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Read(files(t, t.TempDir(), tt.tree))
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
	p, err = Read(files(t, t.TempDir(), map[string]string{"plugin.toml": "name = \"bare\""}))
	require.NoError(t, err)
	assert.Equal(t, "0.0.0", p.Version)
	assert.False(t, p.RunsCode())
}

func TestInstallAndApply(t *testing.T) {
	store := &Store{Dir: t.TempDir()}
	ctx := context.Background()
	st, err := Fetch(ctx, sample(t))
	require.NoError(t, err)
	i, err := store.Install(st)
	require.NoError(t, err)
	assert.True(t, i.Enabled)
	assert.True(t, strings.HasPrefix(i.Hash, "sha256:"))
	_, err = os.Stat(filepath.Join(store.PluginDir(i), ".git"))
	assert.True(t, os.IsNotExist(err), "no .git copied")

	cfg := config.DefaultConfig()
	cfg.MCP.Servers = []config.MCPServerConfig{{Name: "other", Command: "x"}}
	loaded, problems := Apply(cfg, store)
	require.Empty(t, problems)
	require.Len(t, loaded, 1)
	dir := store.PluginDir(i)
	assert.Contains(t, cfg.Skills.Paths, filepath.Join(dir, "skills"))
	assert.Equal(t, []string{filepath.Join(dir, "agents")}, cfg.PluginAgentDirs)
	assert.Equal(t, []config.PluginDir{{Dir: filepath.Join(dir, "commands"), Plugin: "lint-kit"}}, cfg.PluginCommandDirs)
	require.Len(t, cfg.Hooks.PostTool, 1)
	assert.Equal(t, dir+"/bin/check.sh", cfg.Hooks.PostTool[0].Command, "the plugin's root is filled in")
	assert.Equal(t, "plugin lint-kit", cfg.Hooks.PostTool[0].Source)
	require.Len(t, cfg.MCP.Servers, 2)
	assert.Equal(t, []string{"--root", dir}, cfg.MCP.Servers[1].Args)

	// A second MCP server of the same name isn't used.
	cfg = config.DefaultConfig()
	cfg.MCP.Servers = []config.MCPServerConfig{{Name: "lintd", Command: "mine"}}
	_, problems = Apply(cfg, store)
	assert.Contains(t, strings.Join(problems, "\n"), "an MCP server named lintd exists already")

	// Disabled: by the user, then enabled for a workspace, and disabled there.
	require.NoError(t, store.SetEnabled("lint-kit", false))
	for _, tt := range []struct {
		name            string
		enable, disable []string
		want            int
	}{
		{"disabled", nil, nil, 0},
		{"a project enables it", []string{"lint-kit"}, nil, 1},
		{"and disables it", []string{"lint-kit"}, []string{"lint-kit"}, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Plugins.Enable, cfg.Plugins.Disable = tt.enable, tt.disable
			loaded, _ := Apply(cfg, store)
			assert.Len(t, loaded, tt.want)
		})
	}
	assert.ErrorIs(t, store.SetEnabled("nope", true), ErrNotInstalled)

	// Changed after installing: not loaded.
	require.NoError(t, store.SetEnabled("lint-kit", true))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bin", "check.sh"), []byte("#!/bin/sh\ncurl evil\n"), 0o755))
	loaded, problems = Apply(config.DefaultConfig(), store)
	assert.Empty(t, loaded)
	assert.Contains(t, strings.Join(problems, "\n"), "changed since it was installed")

	// --plugin-dir, and an enabled one that isn't installed.
	cfg = config.DefaultConfig()
	cfg.Plugins.Dirs = []string{sample(t), t.TempDir()}
	cfg.Plugins.Enable = []string{"ghost"}
	loaded, problems = Apply(cfg, &Store{Dir: t.TempDir()})
	assert.Len(t, loaded, 1)
	assert.Nil(t, loaded[0].Installed)
	assert.Len(t, problems, 2)

	require.NoError(t, store.Remove("lint-kit"))
	assert.ErrorIs(t, store.Remove("lint-kit"), ErrNotInstalled)
	list, _ := store.List()
	assert.Empty(t, list)
}

// A new version replaces the old, keeping whether it was enabled.
func TestReinstall(t *testing.T) {
	store := &Store{Dir: t.TempDir()}
	src := sample(t)
	st, _ := Fetch(context.Background(), src)
	i1, err := store.Install(st)
	require.NoError(t, err)
	require.NoError(t, store.SetEnabled("lint-kit", false))
	files(t, src, map[string]string{"plugin.toml": "name = \"lint-kit\"\nversion = \"1.3.0\"\n"})
	st, _ = Fetch(context.Background(), src)
	i2, err := store.Install(st)
	require.NoError(t, err)
	assert.Equal(t, "1.3.0", i2.Version)
	assert.False(t, i2.Enabled, "still disabled")
	_, err = os.Stat(store.PluginDir(i1))
	assert.True(t, os.IsNotExist(err), "the old version is gone")
	got, err := store.Get("lint-kit")
	require.NoError(t, err)
	assert.Equal(t, i2, got)
}

func zipOf(t *testing.T, tree map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "p.zip")
	f, err := os.Create(path)
	require.NoError(t, err)
	w := zip.NewWriter(f)
	for name, content := range tree {
		fw, err := w.Create(name)
		require.NoError(t, err)
		fw.Write([]byte(content))
	}
	require.NoError(t, w.Close())
	require.NoError(t, f.Close())
	return path
}

func TestFetchSources(t *testing.T) {
	ctx := context.Background()
	archive := zipOf(t, map[string]string{"kit/plugin.toml": "name = \"zipped\"\n", "kit/commands/a.md": "---\ndescription: a\n---\nA\n"})
	st, err := Fetch(ctx, archive)
	require.NoError(t, err)
	assert.Equal(t, "zipped", st.Name, "the archive's one folder is the plugin")
	st.Discard()

	_, err = Fetch(ctx, zipOf(t, map[string]string{"../escape": "x", "plugin.toml": "name = \"x\""}))
	assert.ErrorContains(t, err, "leaves the archive")

	data, _ := os.ReadFile(archive)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/kit.zip" {
			http.NotFound(w, r)
			return
		}
		w.Write(data)
	}))
	defer srv.Close()
	st, err = Fetch(ctx, srv.URL+"/kit.zip")
	require.NoError(t, err)
	assert.Equal(t, "zipped", st.Name)
	st.Discard()
	_, err = Fetch(ctx, srv.URL+"/missing.zip")
	assert.ErrorContains(t, err, "HTTP 404")

	if _, err := exec.LookPath("git"); err == nil {
		repo := filepath.Join(t.TempDir(), "kit.git")
		files(t, repo, map[string]string{"plugin.toml": "name = \"from-git\"\n"})
		for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-qm", "x"}} {
			cmd := exec.Command("git", args...)
			cmd.Dir = repo
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", out)
		}
		st, err = Fetch(ctx, repo)
		require.NoError(t, err)
		assert.Equal(t, "from-git", st.Name)
		st.Discard()
		_, err = Fetch(ctx, repo+"#no-such-branch")
		assert.ErrorContains(t, err, "git clone")
	}
}

func TestMarketplace(t *testing.T) {
	ctx := context.Background()
	store := &Store{Dir: t.TempDir()}
	market := t.TempDir()
	kit := sample(t)
	require.NoError(t, os.MkdirAll(filepath.Join(market, "plugins"), 0o755))
	require.NoError(t, os.Rename(kit, filepath.Join(market, "plugins", "lint-kit")))
	os.RemoveAll(filepath.Join(market, "plugins", "lint-kit", ".git"))
	hash, err := Hash(filepath.Join(market, "plugins", "lint-kit"))
	require.NoError(t, err)
	index := "name = \"acme\"\n[[plugins]]\nname = \"lint-kit\"\nversion = \"1.2.0\"\nsource = \"plugins/lint-kit\"\nhash = \"" + hash + "\"\n" +
		"[[plugins]]\nname = \"unpinned\"\nsource = \"plugins/unpinned\"\n" +
		"[[plugins]]\nname = \"tampered\"\nsource = \"plugins/tampered\"\nhash = \"sha256:00\"\n" +
		"[[plugins]]\nname = \"liar\"\nsource = \"plugins/lint-kit\"\nhash = \"" + hash + "\"\n"
	files(t, market, map[string]string{
		IndexFile:                      index,
		"plugins/unpinned/plugin.toml": "name = \"unpinned\"\n",
		"plugins/tampered/plugin.toml": "name = \"tampered\"\n",
	})

	_, ok, err := store.FromMarketplace(ctx, "lint-kit")
	assert.False(t, ok, "no marketplaces: not a marketplace name")
	assert.NoError(t, err)

	m, err := store.AddMarketplace(ctx, market)
	require.NoError(t, err)
	assert.Equal(t, "acme", m.Name)
	known, _ := store.Marketplaces()
	assert.Equal(t, []Known{{Name: "acme", URL: market}}, known)

	for _, tt := range []struct {
		ref, wantErr string
	}{
		{ref: "lint-kit"},
		{ref: "lint-kit@acme"},
		{ref: "unpinned", wantErr: "gives no hash"},
		{ref: "tampered", wantErr: "doesn't match the hash"},
		{ref: "liar", wantErr: `holds the plugin "lint-kit"`},
		{ref: "nothing@acme", wantErr: "no marketplace offers"},
		{ref: "lint-kit@elsewhere", wantErr: "no marketplace offers"},
	} {
		t.Run(tt.ref, func(t *testing.T) {
			st, ok, err := store.FromMarketplace(ctx, tt.ref)
			assert.True(t, ok)
			if tt.wantErr != "" {
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			defer st.Discard()
			assert.Equal(t, "lint-kit@acme", st.Source)
			assert.NotContains(t, st.Dir, market, "copied out of the marketplace")
		})
	}
	_, ok, _ = store.FromMarketplace(ctx, market)
	assert.False(t, ok, "a directory isn't a name")

	require.NoError(t, store.RemoveMarketplace("acme"))
	assert.Error(t, store.RemoveMarketplace("acme"))
	_, err = store.AddMarketplace(ctx, t.TempDir())
	assert.ErrorContains(t, err, IndexFile)
}
