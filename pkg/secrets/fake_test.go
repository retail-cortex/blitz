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

package secrets

import (
	"os"
	"path/filepath"
	goruntime "runtime"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSecretTool emulates secret-tool over a directory, one file per
// account; FAKE_FAIL makes every command fail as if no service answered.
const fakeSecretTool = `#!/bin/bash
[ -n "$FAKE_FAIL" ] && { echo "no service" >&2; exit 2; }
case "$1" in
search) exit 1 ;;
lookup) [ -f "$FAKE_STORE/$5" ] || exit 1; cat "$FAKE_STORE/$5"; echo ;;
store) cat > "$FAKE_STORE/$6" ;;
clear) [ -f "$FAKE_STORE/$5" ] || exit 1; rm "$FAKE_STORE/$5" ;;
esac
`

// fakeSecurity emulates macOS's security tool the same way: exit 44 is
// errSecItemNotFound, and -i reads add-generic-password with the value in hex.
const fakeSecurity = `#!/bin/bash
[ -n "$FAKE_FAIL" ] && { echo "security: error" >&2; exit 2; }
case "$1" in
find-generic-password) [ -f "$FAKE_STORE/$5" ] || exit 44; cat "$FAKE_STORE/$5"; echo ;;
delete-generic-password) [ -f "$FAKE_STORE/$5" ] || exit 44; rm "$FAKE_STORE/$5" ;;
-i)
  read -r line
  [[ $line =~ -a\ \"([^\"]*)\" ]] || { echo "error: bad command"; exit 0; }
  name=${BASH_REMATCH[1]}
  hex=${line##* }
  printf '%b' "$(sed 's/../\\x&/g' <<<"$hex")" > "$FAKE_STORE/$name" ;;
esac
`

// fakeTools puts the fake secret-tool on PATH and points security at the
// fake one, with an empty store; it returns the tools' directory.
func fakeTools(t *testing.T) string {
	t.Helper()
	bin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bin, "secret-tool"), []byte(fakeSecretTool), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(bin, "security"), []byte(fakeSecurity), 0o755))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_STORE", t.TempDir())
	t.Setenv("FAKE_FAIL", "")
	old := security
	security = filepath.Join(bin, "security")
	t.Cleanup(func() { security = old })
	return bin
}

// TestKeychain runs the Keychain store against a fake security tool: the
// value goes in hex on stdin and comes back as written.
func TestKeychain(t *testing.T) {
	fakeTools(t)
	assert.Equal(t, "macOS Keychain", keychain{}.Kind())
	exercise(t, keychain{}, "a")
}

// TestKeychainErrors checks failures other than a missing item are reported.
func TestKeychainErrors(t *testing.T) {
	fakeTools(t)
	t.Setenv("FAKE_FAIL", "1")
	_, err := keychain{}.Get("a")
	assert.ErrorContains(t, err, "reading a from the Keychain")
	assert.ErrorContains(t, keychain{}.Set("a", "v"), "saving a in the Keychain")
	assert.Error(t, keychain{}.Delete("a"))
}

// TestQuote checks quotes and backslashes are escaped for security -i.
func TestQuote(t *testing.T) {
	assert.Equal(t, `"a\"b\\c"`, quote(`a"b\c`))
}

// TestSecretService runs the Secret Service store against a fake secret-tool.
func TestSecretService(t *testing.T) {
	fakeTools(t)
	assert.Equal(t, "Secret Service", secretService{}.Kind())
	exercise(t, secretService{}, "a")
}

// TestSecretServiceErrors checks failures other than a missing item are
// reported, with secret-tool's message.
func TestSecretServiceErrors(t *testing.T) {
	fakeTools(t)
	t.Setenv("FAKE_FAIL", "1")
	_, err := secretService{}.Get("a")
	assert.ErrorContains(t, err, "reading a from the Secret Service")
	assert.ErrorContains(t, secretService{}.Set("a", "v"), "no service")
	assert.Error(t, secretService{}.Delete("a"))
}

// TestDefault checks the store chosen for this machine: the Keychain on
// macOS; on Linux the Secret Service when one answers, else the file.
func TestDefault(t *testing.T) {
	old := defaultStore
	t.Cleanup(func() { defaultOnce, defaultStore = sync.Once{}, old })
	dir := t.TempDir()
	fakeTools(t)

	cases := map[string]struct {
		fail  string
		path  bool
		linux Store
	}{
		"service answers": {path: true, linux: secretService{}},
		"service fails":   {path: true, fail: "1", linux: &FileStore{Path: filepath.Join(dir, "secrets.toml")}},
		"no secret-tool":  {linux: &FileStore{Path: filepath.Join(dir, "secrets.toml")}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("FAKE_FAIL", tc.fail)
			if !tc.path {
				t.Setenv("PATH", t.TempDir())
			}
			defaultOnce = sync.Once{}
			got := Default(dir)
			switch goruntime.GOOS {
			case "darwin":
				assert.Equal(t, keychain{}, got)
			case "linux":
				assert.Equal(t, tc.linux, got)
			}
			assert.Equal(t, got, Default("elsewhere"), "chosen once")
		})
	}

	m := &Memory{}
	SetDefault(m)
	assert.Same(t, m, Default(dir))
}

// TestFileStoreErrors checks a file that isn't TOML is reported by every
// operation, and a directory Set can't make or write.
func TestFileStoreErrors(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "secrets.toml")
	require.NoError(t, os.WriteFile(bad, []byte("not = = toml"), 0o600))
	s := &FileStore{Path: bad}
	assert.Equal(t, "file ("+bad+")", s.Kind())
	_, err := s.Get("a")
	assert.Error(t, err, "get")
	assert.Error(t, s.Set("a", "v"), "set")
	assert.Error(t, s.Delete("a"), "delete")

	if os.Geteuid() != 0 { // root writes anywhere
		ro := filepath.Join(dir, "ro")
		require.NoError(t, os.Mkdir(ro, 0o500))
		assert.Error(t, (&FileStore{Path: filepath.Join(ro, "secrets.toml")}).Set("a", "v"), "a directory it can't write")
		assert.Error(t, (&FileStore{Path: filepath.Join(ro, "sub", "secrets.toml")}).Set("a", "v"), "a directory it can't make")
	}
}

// TestMemoryKind checks the in-memory store's name.
func TestMemoryKind(t *testing.T) {
	assert.Equal(t, "memory", (&Memory{}).Kind())
}
