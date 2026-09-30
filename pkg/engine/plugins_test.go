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

package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An installed plugin's command, agent and hook are there when the
// workspace opens.
func TestPluginsLoad(t *testing.T) {
	w, _ := openTestWith(t, func(c *config.Config) {
		src := filepath.Join(t.TempDir(), "kit")
		for p, content := range map[string]string{
			"plugin.toml":       "name = \"kit\"\n",
			"commands/greet.md": "---\ndescription: Greet\n---\nGreet $ARGUMENTS.\n",
			"agents/greeter.md": "---\nname: greeter\ndescription: Greets people\ntools: [read_file]\n---\nYou greet.\n",
			"hooks.toml":        "[[stop]]\nargs = [\"${BLITZ_PLUGIN_ROOT}/stop.sh\"]\n",
		} {
			require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(src, p)), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(src, p), []byte(content), 0o644))
		}
		st, err := plugins.Fetch(context.Background(), src)
		require.NoError(t, err)
		_, err = plugins.Default().Install(st)
		require.NoError(t, err)
	})

	var sources []string
	for _, c := range w.ListCommands() {
		if c.Name == "greet" {
			sources = append(sources, c.Source)
		}
	}
	assert.Equal(t, []string{"plugin kit"}, sources)
	var agents []string
	for _, a := range w.ListAgents() {
		agents = append(agents, a.Name)
	}
	assert.Contains(t, agents, "greeter")
	hooks := w.ListHooks()
	require.Len(t, hooks, 1)
	assert.Equal(t, "plugin kit", hooks[0].Source)
	assert.Equal(t, filepath.Join(plugins.Default().Dir, "kit", "0.0.0", "stop.sh"), hooks[0].Runs)
}
