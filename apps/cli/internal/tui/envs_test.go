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

package tui

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnvsListPruneRemove(t *testing.T) {
	app, _ := newCommandApp(t, "") // sets HOME to a temporary directory
	home := os.Getenv("HOME")
	python, err := tools.SystemPython()
	if err != nil {
		t.Skip(err)
	}
	skillsDir := t.TempDir()
	os.MkdirAll(filepath.Join(skillsDir, "s"), 0o755)
	os.WriteFile(filepath.Join(skillsDir, "s", "SKILL.md"), []byte("---\nname: s\nscripts:\n  - name: r\n    language: python\n    inline_code: x\n    dependencies: [\"six==1.16.0\"]\n---\n"), 0o644)
	require.NoError(t, local(app).Skills().DiscoverExternal([]string{skillsDir}))
	envs := local(app).Tools().SkillScripts().Envs()
	dir := filepath.Join(home, ".blitz", "envs")
	mk := func(key string, marker bool, deps ...string) {
		os.MkdirAll(filepath.Join(dir, key, "lib"), 0o700)
		os.WriteFile(filepath.Join(dir, key, "lib", "pkg.py"), make([]byte, 4096), 0o600)
		if marker {
			b, _ := json.Marshal(map[string]any{"key": key, "deps": deps, "skills": []string{"s"}, "last_used": time.Now()})
			os.WriteFile(filepath.Join(dir, key, ".blitz-env.json"), b, 0o600)
		}
	}
	needed := envs.Key(python, []string{"six==1.16.0"})
	mk(needed, true, "six==1.16.0")
	mk("0123456789abcdef", true, "requests")
	mk("fedcba9876543210", false)
	run := func(cmd string) string {
		return captureStdout(t, func() { HandleCommand(context.Background(), cmd, app) })
	}

	out := run("/envs")
	for _, want := range []string{"Script environments (3)", needed, "six==1.16.0", "used by s", "incomplete"} {
		assert.Contains(t, out, want, "list lacks %q:\n%s", want, out)
	}
	out = run("/envs prune")
	assert.Contains(t, out, "Removed 2 environment(s)", "prune:\n%s", out)
	left, _ := os.ReadDir(dir)
	require.Len(t, left, 1, "after prune: %v", left)
	require.Equal(t, needed, left[0].Name(), "after prune: %v", left)
	out = run("/envs remove " + needed)
	assert.Contains(t, out, "Removed environment", "remove:\n%s", out)
	out = run("/envs remove ../x")
	assert.Contains(t, out, "Could not remove", "bad key:\n%s", out)
	out = run("/envs")
	assert.Contains(t, out, "No script environments", "empty:\n%s", out)
	out = run("/envs bogus")
	assert.Contains(t, out, "Usage: /envs", "usage:\n%s", out)
}
