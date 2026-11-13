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

package lsp

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeAt(t *testing.T, path, text string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(text), 0o644))
}

// MODULE.bazel names the rules (rules_go by its repo_name when it has
// one); an older WORKSPACE file says so too; neither, not Bazel.
func TestReadBazel(t *testing.T) {
	for name, tc := range map[string]struct {
		file, text string
		want       bazelWorkspace
	}{
		"module": {"MODULE.bazel", `module(name = "acme")
bazel_dep(name = "rules_go", version = "0.63.0")
bazel_dep(
    name = "rules_python",
    version = "1.6.0",
)`, bazelWorkspace{ok: true, rulesGo: "rules_go", rulesPython: true}},
		"repo name": {"MODULE.bazel", `bazel_dep(name = "rules_go", version = "0.63.0", repo_name = "io_bazel_rules_go")`,
			bazelWorkspace{ok: true, rulesGo: "io_bazel_rules_go"}},
		"no rules":  {"MODULE.bazel", `module(name = "acme")`, bazelWorkspace{ok: true}},
		"workspace": {"WORKSPACE", `http_archive(name = "io_bazel_rules_go", urls = ["…"])`, bazelWorkspace{ok: true, rulesGo: "io_bazel_rules_go"}},
		"none":      {"go.mod", "module x\n", bazelWorkspace{}},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeAt(t, filepath.Join(dir, tc.file), tc.text)
			assert.Equal(t, tc.want, readBazel(dir))
		})
	}
}

// gopls gets rules_go's package driver, with Bazel's own output base, for
// sources with no go.mod above them; the workspace's own driver when it
// has one; nothing where go.mod serves, or without rules_go.
func TestGoDriverEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the driver is a shell script")
	}
	cache := t.TempDir()
	old := cacheDir
	cacheDir = func() string { return cache }
	t.Cleanup(func() { cacheDir = old })

	root := t.TempDir()
	writeAt(t, filepath.Join(root, "MODULE.bazel"), `bazel_dep(name = "rules_go", version = "0.63.0")`)
	writeAt(t, filepath.Join(root, "third_party", "go", "go.mod"), "module acme\n")
	m := NewManager(root, nil, nil)
	env := m.goDriverEnv(root)
	script := filepath.Join(cache, "lsp", "gopackagesdriver.sh")
	require.Len(t, env, 3)
	assert.Equal(t, "GOPACKAGESDRIVER="+script, env[0])
	assert.Regexp(t, `^GOPACKAGESDRIVER_BAZEL_FLAGS=--output_base=`+regexp.QuoteMeta(filepath.Join(cache, "bazel"))+`/[0-9a-f]{12} --max_idle_secs=1800$`, env[1])
	assert.Equal(t, "BLITZ_RULES_GO=rules_go", env[2])
	data, err := os.ReadFile(script)
	require.NoError(t, err)
	assert.Equal(t, goDriverScript, string(data))
	assert.Equal(t, env, m.goDriverEnv(root), "the same each time")

	assert.Nil(t, m.goDriverEnv(filepath.Join(root, "third_party", "go")), "go.mod serves its module")

	writeAt(t, filepath.Join(root, "tools", "gopackagesdriver.sh"), "#!/bin/sh\n")
	assert.Equal(t, []string{"GOPACKAGESDRIVER=" + filepath.Join(root, "tools", "gopackagesdriver.sh")}, m.goDriverEnv(root), "the workspace's own")

	plain := t.TempDir()
	writeAt(t, filepath.Join(plain, "MODULE.bazel"), `module(name = "x")`)
	assert.Nil(t, NewManager(plain, nil, nil).goDriverEnv(plain), "no rules_go")
}

// In a Bazel workspace, TypeScript comes from Bazel's output (rules_js),
// Python's roots are its pyproject.toml files, not requirements locks,
// and pip's packages in Bazel's external repositories are pyright's extra
// paths.
func TestBazelTypeScriptAndPython(t *testing.T) {
	root := t.TempDir()
	writeAt(t, filepath.Join(root, "MODULE.bazel"), `bazel_dep(name = "rules_python", version = "1.6.0")
bazel_dep(name = "aspect_rules_ts", version = "3.7.0")`)
	writeAt(t, filepath.Join(root, "bazel-bin", "web", "node_modules", "typescript", "lib", "tsserver.js"), "")
	writeAt(t, filepath.Join(root, "web", "src", "a.ts"), "")
	writeAt(t, filepath.Join(root, "py", "requirements_lock.txt"), "requests==2.32.3\n")
	writeAt(t, filepath.Join(root, "py", "tools", "a.py"), "")
	writeAt(t, filepath.Join(root, "svc", "pyproject.toml"), "")
	site := filepath.Join(root, "bazel-"+filepath.Base(root), "external", "rules_python++pip+pypi_312_requests", "site-packages")
	require.NoError(t, os.MkdirAll(site, 0o755))
	m := NewManager(root, nil, nil)

	assert.Equal(t, filepath.Join(root, "bazel-bin", "web", "node_modules", "typescript", "lib"), m.bazelTSLib(filepath.Join(root, "web", "src")))
	assert.Equal(t, "", m.bazelTSLib(filepath.Join(root, "py")))

	assert.Equal(t, root, m.projectRoot("python", filepath.Join(root, "py", "tools", "a.py")), "imports start at the workspace")
	assert.Equal(t, filepath.Join(root, "svc"), m.projectRoot("python", filepath.Join(root, "svc", "b.py")))
	assert.Equal(t, []string{site}, m.sitePackages())

	assert.Equal(t, []string{"bazel-bin", "bazel-out", "bazel-testlogs", "bazel-" + filepath.Base(root)}, m.bazelOutputs())
	assert.Equal(t, "", NewManager(t.TempDir(), nil, nil).bazelTSLib(root), "not a Bazel workspace")
}
