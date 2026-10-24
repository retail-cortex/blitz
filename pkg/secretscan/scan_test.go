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

package secretscan

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The keys are put together here so this file holds none a scanner
// (this one, or a host's push protection) would stop.
var (
	awsKey       = "AKIA" + "Q3EXAMPLEZ7Y4WXN"
	anthropicKey = "sk-" + "ant-" + "api03-" + strings.Repeat("aB3dE6gH9", 3)
	googleKey    = "AIza" + "SyD-" + strings.Repeat("x7Kq9Lm2Pz", 2) + "Abcd1Ef2Gh3"
	githubToken  = "ghp" + "_" + strings.Repeat("A1b2C3d4E5", 4)
	privateKey   = "-----BEGIN " + "RSA PRIVATE KEY-----"
	random       = "f8Kz2Qp9Lm4Xv7Rt1Nc6Wb3Hy5"
)

func TestScan(t *testing.T) {
	tests := []struct {
		name, file, text string
		kinds            []string
	}{
		{"an AWS key in code", "main.go", `const id = "` + awsKey + `"`, []string{"AWS access key"}},
		{"an Anthropic key in a script", "run.sh", "export ANTHROPIC_API_KEY=" + anthropicKey, []string{"Anthropic API key"}},
		{"a Google key in JSON", "config.json", `{"key": "` + googleKey + `"}`, []string{"Google API key"}},
		{"a GitHub token in YAML", "ci.yml", "token: " + githubToken, []string{"GitHub token"}},
		{"a private key", "id_rsa", privateKey + "\nMIIE...", []string{"private key"}},
		{"a random value for a key-like name in .env", ".env", "DB_PASSWORD=" + random, []string{"secret value"}},
		{"in .env.local too", ".env.local", "API_TOKEN='" + random + "'", []string{"secret value"}},
		{"several on their lines", "a.env", "A_SECRET=" + random + "\nB=1\nAWS=" + awsKey, []string{"secret value", "AWS access key"}},

		{"the same in code is fine", "auth.go", "token = " + random, nil},
		{"a placeholder", ".env", "API_KEY=${API_KEY_FROM_VAULT}\nPASSWORD=changeme-please-1234", nil},
		{"a keychain reference", "settings.toml", `api_key = "keychain:gemini-api-key-main"`, nil},
		{"an example file", ".env.example", "API_KEY=" + random, nil},
		{"a short or plain value", ".env", "TOKEN_TTL=3600\nSECRET_NAME=production-database-password", nil},
		{"allowed on its line", "fixtures.go", `const fake = "` + awsKey + `" // ` + AllowMarker, nil},
		{"binary", "logo.png", "\x89PNG\x00" + awsKey, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Scan(tt.file, []byte(tt.text))
			var kinds []string
			for _, f := range got {
				kinds = append(kinds, f.Kind)
				assert.Equal(t, tt.file, f.Path)
				assert.NotContains(t, tt.text[:0]+f.Masked, random[4:], "masked")
				assert.True(t, strings.HasSuffix(f.Masked, "•"), "masked: %q", f.Masked)
			}
			assert.Equal(t, tt.kinds, kinds)
		})
	}
	assert.Equal(t, 3, Scan("a.env", []byte("A_SECRET="+random+"\nB=1\nAWS="+awsKey))[1].Line)
}

func needGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
}

// git runs git in dir, without the user's or system's config, and fails
// the test on an error.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitCmd(dir, args...).CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return strings.TrimSpace(string(out))
}

func gitCmd(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", append([]string{"-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
	cmd.Dir = dir
	// No blitz on PATH: the hook's own search runs.
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "PATH=/usr/bin:/bin")
	return cmd
}

func TestInstall(t *testing.T) {
	needGit(t)
	ctx := context.Background()
	repo := func(t *testing.T) string {
		dir := t.TempDir()
		git(t, dir, "init", "-q")
		return dir
	}
	t.Run("installs, then is present", func(t *testing.T) {
		dir := repo(t)
		r, err := Install(ctx, filepath.Join(dir))
		require.NoError(t, err)
		assert.Equal(t, Installed, r.Status)
		info, err := os.Stat(filepath.Join(dir, HooksDir, "pre-commit"))
		require.NoError(t, err)
		assert.NotZero(t, info.Mode()&0o100, "executable")
		assert.Equal(t, HooksDir, git(t, dir, "config", "--local", "core.hooksPath"))
		r, err = Install(ctx, dir)
		require.NoError(t, err)
		assert.Equal(t, Present, r.Status)
	})
	t.Run("from a folder inside the repository", func(t *testing.T) {
		dir := repo(t)
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o755))
		r, err := Install(ctx, filepath.Join(dir, "sub"))
		require.NoError(t, err)
		assert.Equal(t, Installed, r.Status)
		assert.FileExists(t, filepath.Join(dir, HooksDir, "pre-commit"))
	})
	for name, setup := range map[string]func(t *testing.T, dir string){
		"core.hooksPath elsewhere": func(t *testing.T, dir string) { git(t, dir, "config", "core.hooksPath", ".hooks") },
		"husky":                    func(t *testing.T, dir string) { require.NoError(t, os.MkdirAll(filepath.Join(dir, ".husky"), 0o755)) },
		"the pre-commit framework": func(t *testing.T, dir string) {
			require.NoError(t, os.WriteFile(filepath.Join(dir, ".pre-commit-config.yaml"), []byte("repos: []\n"), 0o644))
		},
		"a hook of its own": func(t *testing.T, dir string) {
			require.NoError(t, os.WriteFile(filepath.Join(dir, ".git", "hooks", "pre-commit"), []byte("#!/bin/sh\n"), 0o755))
		},
	} {
		t.Run("leaves other hooks alone: "+name, func(t *testing.T) {
			dir := repo(t)
			setup(t, dir)
			r, err := Install(ctx, dir)
			require.NoError(t, err)
			assert.Equal(t, OtherHooks, r.Status)
			assert.NotEmpty(t, r.Detail)
			assert.NoFileExists(t, filepath.Join(dir, HooksDir, "pre-commit"))
		})
	}
	t.Run("not a repository", func(t *testing.T) {
		_, err := Install(ctx, t.TempDir())
		assert.ErrorIs(t, err, ErrNotARepository)
	})
}

// A hook Blitz wrote before (an older version) is brought up to date; a
// hook of the repository's own in .githooks is left alone.
func TestInstallUpdatesItsOwnHook(t *testing.T) {
	needGit(t)
	ctx := context.Background()
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	git(t, dir, "config", "core.hooksPath", HooksDir)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, HooksDir), 0o755))
	old := hookHeader + "exec blitz security scan-secrets --staged\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, HooksDir, "pre-commit"), []byte(old), 0o755))
	r, err := Install(ctx, dir)
	require.NoError(t, err)
	assert.Equal(t, Installed, r.Status, "updated")
	data, _ := os.ReadFile(filepath.Join(dir, HooksDir, "pre-commit"))
	assert.Equal(t, HookScript, string(data))

	theirs := t.TempDir()
	git(t, theirs, "init", "-q")
	require.NoError(t, os.MkdirAll(filepath.Join(theirs, HooksDir), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(theirs, HooksDir, "pre-commit"), []byte("#!/bin/sh\nmake lint\n"), 0o755))
	r, err = Install(ctx, theirs)
	require.NoError(t, err)
	assert.Equal(t, OtherHooks, r.Status)
}

// An older blitz on PATH, without the scan, isn't run: the hook's own
// search does the work (the owner's report: "unknown flag: --staged").
func TestHookWithAnOlderBlitz(t *testing.T) {
	needGit(t)
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	bin := t.TempDir()
	// An old blitz: `security --help` lists only fix-apparmor, and anything
	// else fails as it did.
	require.NoError(t, os.WriteFile(filepath.Join(bin, "blitz"), []byte("#!/bin/sh\nif [ \"$2\" = --help ]; then echo 'Available Commands:'; echo '  fix-apparmor'; exit 0; fi\necho 'Error: unknown flag: --staged' >&2\nexit 1\n"), 0o755))
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	_, err := Install(context.Background(), dir)
	require.NoError(t, err)
	commit := func(file, text string) ([]byte, error) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, file), []byte(text), 0o644))
		git(t, dir, "add", file)
		cmd := gitCmd(dir, "commit", "-qm", file)
		cmd.Env = append(cmd.Env, "PATH="+bin+":/usr/bin:/bin")
		return cmd.CombinedOutput()
	}
	out, err := commit("ok.go", "package ok\n")
	require.NoError(t, err, "a clean commit goes through: %s", out)
	assert.NotContains(t, string(out), "unknown flag")
	out, err = commit("leak.go", `const k = "`+awsKey+`"`+"\n")
	require.Error(t, err)
	assert.Contains(t, string(out), "looks like an API key")
}

// The hook, without blitz on PATH, stops a commit that adds a key and lets
// the rest through.
func TestHookStopsACommit(t *testing.T) {
	needGit(t)
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	_, err := Install(context.Background(), dir)
	require.NoError(t, err)
	commit := func(file, text string) ([]byte, error) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, file), []byte(text), 0o644))
		git(t, dir, "add", file)
		return gitCmd(dir, "commit", "-qm", file).CombinedOutput()
	}
	out, err := commit("ok.go", "package ok\n")
	require.NoError(t, err, "%s", out)
	out, err = commit("leak.go", `const k = "`+awsKey+`"`+"\n")
	require.Error(t, err, "the commit is stopped")
	assert.Contains(t, string(out), "looks like an API key")
	assert.Contains(t, string(out), "AKIA••••", "masked")
	assert.NotContains(t, string(out), awsKey, "never the key itself")
	out, err = commit("leak.go", `const k = "`+awsKey+`" // `+AllowMarker+"\n")
	require.NoError(t, err, "allowed on its line: %s", out)
}

func TestStaged(t *testing.T) {
	needGit(t)
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("staged\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("not staged\n"), 0o644))
	git(t, dir, "add", "a.txt")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed after\n"), 0o644))
	got, err := Staged(context.Background(), dir)
	require.NoError(t, err)
	assert.Equal(t, map[string][]byte{"a.txt": []byte("staged\n")}, got, "what's staged, as staged")
	_, err = Staged(context.Background(), t.TempDir())
	assert.ErrorIs(t, err, ErrNotARepository)
}
