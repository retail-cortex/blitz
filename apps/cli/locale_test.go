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
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

func lastSystemText(m *runtime.MockLLM) string {
	req := m.Requests[len(m.Requests)-1]
	var sb strings.Builder
	if req.Config != nil && req.Config.SystemInstruction != nil {
		for _, p := range req.Config.SystemInstruction.Parts {
			sb.WriteString(p.Text)
		}
	}
	return sb.String()
}

// /locale es must reach the model (reply in Spanish), survive a restart via
// the config file, and switching back to English must remove the instruction.
func TestLocaleReachesModelAndConfig(t *testing.T) {
	defer i18n.SetCurrent(nil)
	ctx := context.Background()
	e := testEnv(t)
	cfgDir := t.TempDir()
	t.Setenv("MODENV_PREFIX", cfgDir)
	cfgFile := filepath.Join(cfgDir, ".env.toml")
	os.WriteFile(cfgFile, []byte("# my settings\n[ui]\nlocale = \"en-US\"   # keep me\n"), 0o600)

	reply := func() *genai.Content { return genai.NewContentFromText("ok", genai.RoleModel) }
	llm := runtime.NewMockLLM("gemini-3.8-flash", reply(), reply())
	require.NoError(t, e.Engine().SetModel(ctx, llm))
	run := func() string {
		sess, _ := e.Storage().CreateSession("", "t", "blitz")
		var out bytes.Buffer
		require.NoError(t, runOneShot(ctx, e, oneShotOptions{prompt: "hi", sessionID: sess.ID, format: formatText, stdout: &out}))
		return lastSystemText(llm)
	}

	res, err := e.SetLocale(ctx, "ES-sp")
	require.NoError(t, err, "SetLocale = %+v,", res)
	require.Equal(t, "es", res.Tag, "SetLocale = %+v, %v", res, err)
	require.Equal(t, cfgFile, res.Saved.Path, "SetLocale = %+v, %v", res, err)
	require.NoError(t, res.Saved.Err, "SetLocale = %+v, %v", res, err)
	sys := run()
	assert.Contains(t, sys, "## Response Language", "system prompt lacks the reply-language instruction:\n%s", sys)
	assert.Contains(t, sys, "Spanish", "system prompt lacks the reply-language instruction:\n%s", sys)
	data, _ := os.ReadFile(cfgFile)
	assert.Contains(t, string(data), "# my settings\n[ui]\nlocale = \"es\"   # keep me", "config not edited in place:\n%s", data)

	// Restart: the saved locale is picked up.
	i18n.SetCurrent(nil)
	cfg, err := config.Load(cfgDir)
	require.NoError(t, err)
	engine.SetupLocale(cfg, func(string) {})
	got := i18n.Current().Tag().String()
	assert.Equal(t, "es", got, "after reload: %s", got)
	assert.Equal(t, "Salida cancelada.", i18n.T("exit.cancelled"), "after reload: %s", got)

	_, err = e.SetLocale(ctx, "en-US")
	require.NoError(t, err)
	sys = run()
	assert.NotContains(t, sys, "Response Language", "English should not add a language instruction:\n%s", sys)
}
