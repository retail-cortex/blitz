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

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Output styles and --append-system-prompt reach the agent's instructions.
func TestStylesAndAppendedPrompt(t *testing.T) {
	w, llm := openTestWith(t, func(c *config.Config) { c.UI.Style = "concise" }, text("a"), text("b"), text("c"))
	require.NoError(t, os.MkdirAll(StylesDir(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(StylesDir(), "pirate.md"), []byte("---\ndescription: Arr\n---\nAnswer like a pirate.\n"), 0o644))

	ctx := context.Background()
	s, _ := w.NewSession()
	system := func() string {
		_, err := w.Run(ctx, s.ID, api.Turn{Text: "hi"}, func(api.Event) {})
		require.NoError(t, err)
		return systemText(llm.Requests[len(llm.Requests)-1].Config)
	}
	assert.Equal(t, "concise", w.Settings().Style, "[ui] style")
	assert.Contains(t, system(), "Output style: concise")

	var names []string
	for _, st := range w.ListStyles() {
		names = append(names, st.Name)
		if st.Name == "pirate" {
			assert.Equal(t, "Arr", st.Description)
		}
	}
	assert.Equal(t, []string{"concise", "default", "explanatory", "pirate"}, names)

	_, err := w.Set(ctx, "style", "pirate")
	require.NoError(t, err)
	sys := system()
	assert.Contains(t, sys, "Answer like a pirate.")
	assert.NotContains(t, sys, "Output style: concise")

	_, err = w.Set(ctx, "style", "nope")
	var invalid *api.InvalidSettingError
	assert.ErrorAs(t, err, &invalid)
	_, err = w.Set(ctx, "style", "")
	require.NoError(t, err)
	assert.Equal(t, DefaultStyle, w.Settings().Style)
	assert.NotContains(t, system(), "pirate")
}

func TestAppendSystemPrompt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Session.StorageDir = t.TempDir()
	cfg.UI.Style = "nonsense"
	var warnings []string
	llm := runtime.NewMockLLM("gemini-3.8-flash", text("ok"))
	w, err := Open(context.Background(), cfg, Options{Model: llm, NewModel: mockModels, AppendSystemPrompt: "Always answer in haiku.", Warn: func(s string) { warnings = append(warnings, s) }})
	require.NoError(t, err)
	defer w.Close()
	assert.Contains(t, warnings, `ui.style "nonsense": no such output style (default, concise, explanatory, or ~/.blitz/styles/<name>.md)`)
	s, _ := w.NewSession()
	_, err = w.Run(context.Background(), s.ID, api.Turn{Text: "hi"}, func(api.Event) {})
	require.NoError(t, err)
	assert.Contains(t, systemText(llm.Requests[0].Config), "Always answer in haiku.")
}
