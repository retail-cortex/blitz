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
	if err := e.Engine().SetModel(ctx, llm); err != nil {
		t.Fatal(err)
	}
	run := func() string {
		sess, _ := e.Storage().CreateSession("", "t", "blitz")
		var out bytes.Buffer
		if err := runOneShot(ctx, e, oneShotOptions{prompt: "hi", sessionID: sess.ID, format: formatText, stdout: &out}); err != nil {
			t.Fatal(err)
		}
		return lastSystemText(llm)
	}

	res, err := e.SetLocale(ctx, "ES-sp")
	if err != nil || res.Tag != "es" || res.Saved.Path != cfgFile || res.Saved.Err != nil {
		t.Fatalf("SetLocale = %+v, %v", res, err)
	}
	if sys := run(); !strings.Contains(sys, "## Response Language") || !strings.Contains(sys, "Spanish") {
		t.Errorf("system prompt lacks the reply-language instruction:\n%s", sys)
	}
	data, _ := os.ReadFile(cfgFile)
	if !strings.Contains(string(data), "# my settings\n[ui]\nlocale = \"es\"   # keep me") {
		t.Errorf("config not edited in place:\n%s", data)
	}

	// Restart: the saved locale is picked up.
	i18n.SetCurrent(nil)
	cfg, err := config.Load(cfgDir)
	if err != nil {
		t.Fatal(err)
	}
	engine.SetupLocale(cfg, func(string) {})
	if got := i18n.Current().Tag().String(); got != "es" || i18n.T("exit.cancelled") != "Salida cancelada." {
		t.Errorf("after reload: %s", got)
	}

	if _, err := e.SetLocale(ctx, "en-US"); err != nil {
		t.Fatal(err)
	}
	if sys := run(); strings.Contains(sys, "Response Language") {
		t.Errorf("English should not add a language instruction:\n%s", sys)
	}
}
