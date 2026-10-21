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

package runtime

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/redact"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A shell script's one line is the result; anything else is an error
// saying what went wrong (MDL-102).
func TestRunScriptShell(t *testing.T) {
	ctx := context.Background()
	cases := map[string]struct {
		src, want, wantErr string
	}{
		"one line":         {src: "echo '  tok-1  '", want: "tok-1"},
		"default language": {src: "echo tok-2", want: "tok-2"},
		"fails":            {src: "echo oops >&2; exit 3", wantErr: "failed: exit status 3: oops"},
		"prints nothing":   {src: "true", wantErr: "printed nothing"},
		"two lines":        {src: "printf 'a\\nb\\n'", wantErr: "more than one line"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			lang := config.ScriptShell
			if name == "default language" {
				lang = ""
			}
			got, err := runScript(ctx, lang, tc.src)
			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// A script that runs too long is stopped and says so.
func TestRunScriptTimeout(t *testing.T) {
	old := scriptTimeout
	scriptTimeout = 50 * time.Millisecond
	t.Cleanup(func() { scriptTimeout = old })
	_, err := runScript(context.Background(), config.ScriptShell, "sleep 5")
	assert.ErrorContains(t, err, "timed out after 50ms")
}

// A failed script's standard error is quoted with remembered tokens
// masked, and cut short.
func TestRunScriptStderr(t *testing.T) {
	redact.Remember("tok-remembered-secret")
	_, err := runScript(context.Background(), config.ScriptShell, "echo 'bad tok-remembered-secret' >&2; exit 1")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "tok-remembered-secret", "a remembered token leaked into the error")

	long := scriptStderr(strings.Repeat("é", maxScriptStderr+10))
	assert.Equal(t, maxScriptStderr+1, len([]rune(long)), "cut at the limit, with an ellipsis")
	assert.True(t, strings.HasSuffix(long, "…"))
}

// Each language needs its interpreter, and says which one is missing; an
// unknown language is refused.
func TestScriptCommandInterpreters(t *testing.T) {
	old := lookPath
	lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	t.Cleanup(func() { lookPath = old })
	for lang, want := range map[string]string{
		config.ScriptPython:     "need python3",
		config.ScriptTypeScript: "need node (22.6 or later)",
		"ruby":                  `unknown script language "ruby"`,
	} {
		t.Run(lang, func(t *testing.T) {
			_, err := runScript(context.Background(), lang, "x")
			assert.ErrorContains(t, err, want)
		})
	}
}

// The TypeScript script is written to a file node can strip types from,
// and the file is removed afterwards.
func TestScriptCommandTypeScriptFile(t *testing.T) {
	old := lookPath
	lookPath = func(string) (string, error) { return "/usr/bin/node", nil }
	t.Cleanup(func() { lookPath = old })
	argv, cleanup, err := scriptCommand(config.ScriptTypeScript, "const t: string = 'x'; console.log(t)")
	require.NoError(t, err)
	file := argv[len(argv)-1]
	assert.Equal(t, []string{"/usr/bin/node", "--experimental-strip-types", "--no-warnings"}, argv[:3])
	data, err := os.ReadFile(file)
	require.NoError(t, err)
	assert.Contains(t, string(data), "const t: string")
	cleanup()
	assert.NoFileExists(t, file)
}

// Python and TypeScript scripts run with the real interpreters, when the
// test's PATH has them.
func TestRunScriptRealInterpreters(t *testing.T) {
	ctx := context.Background()
	t.Run("python", func(t *testing.T) {
		if _, err := exec.LookPath("python3"); err != nil {
			t.Skip("no python3 on PATH")
		}
		got, err := runScript(ctx, config.ScriptPython, "import sys\nprint('py-' + str(sys.version_info[0]))")
		require.NoError(t, err)
		assert.Equal(t, "py-3", got)
	})
	t.Run("typescript", func(t *testing.T) {
		if !nodeStripsTypes(t) {
			t.Skip("no node 22.6 or later on PATH")
		}
		got, err := runScript(ctx, config.ScriptTypeScript, "const token: string = 'ts-' + (1 + 1);\nconsole.log(token);")
		require.NoError(t, err)
		assert.Equal(t, "ts-2", got)
	})
}

// nodeStripsTypes reports whether PATH's node is 22.6 or later.
func nodeStripsTypes(t *testing.T) bool {
	t.Helper()
	out, err := exec.Command("node", "--version").Output()
	if err != nil {
		return false
	}
	m := regexp.MustCompile(`^v(\d+)\.(\d+)`).FindStringSubmatch(strings.TrimSpace(string(out)))
	if m == nil {
		return false
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	return major > 22 || (major == 22 && minor >= 6)
}
