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

package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/skills"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const remoteSource = "print('from storage')\n"

// remoteFixture is a skill whose scripts come from a TLS server's
// storage_uri; pin is the SHA-256 the definition pins.
func remoteFixture(t *testing.T, pin string, web bool) (*SkillScripts, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write([]byte(remoteSource))
	}))
	t.Cleanup(srv.Close)
	skillsDir := t.TempDir()
	os.MkdirAll(filepath.Join(skillsDir, "remote"), 0o755)
	doc := "---\nname: remote\nscripts:\n  - name: fetched\n    language: python\n    storage_uri: " + srv.URL + "/scripts/fetched.py\n    storage_sha256: " + pin + "\n" +
		"resources:\n  - id: guide\n    name: Style guide\n    category: RESOURCE_CATEGORY_DOCUMENT_TEXT\n    storage: {gcs_uri: gs://assets/guide.pdf, size_bytes: 2048, mime_type: application/pdf}\n    interpretation: {summary: How we write, interpreted_text: Use short sentences.}\n    auto_inject_context: true\n  - id: note\n    inline_content: Remember the tabs.\n---\n"
	os.WriteFile(filepath.Join(skillsDir, "remote", "SKILL.md"), []byte(doc), 0o644)
	prov, _ := skills.NewProvider()
	require.NoError(t, prov.DiscoverExternal([]string{skillsDir}))
	ws, err := OpenWorkspace(WorkspaceOptions{Dir: t.TempDir()})
	require.NoError(t, err)
	t.Cleanup(func() { ws.Close() })
	hooks, _ := approverHooks(true)
	policy := config.DefaultConfig().Skills.Policy
	r := NewSkillScripts(prov, policy, ws, hooks, NewPyEnvs(filepath.Join(t.TempDir(), "envs"), policy.Packages),
		ScriptBoxConfig{Mode: os.Getenv("BLITZ_PYENV_SANDBOX"), Blocked: ws.Blocked(), StateDir: t.TempDir()})
	r.cacheDir = t.TempDir()
	if web {
		r.SetWeb(WebFetchConfig{AllowPrivate: true, AllowNetwork: true})
		r.web.client.Transport = srv.Client().Transport // trusts the test server
	}
	return r, &hits
}

// A storage_uri script is fetched under the web rules, checked against its
// pinned hash, cached, and run (BL-SK-04).
func TestStorageURIScripts(t *testing.T) {
	if _, err := SystemPython(); err != nil {
		t.Skip(err)
	}
	pin := hexSHA256([]byte(remoteSource))
	r, hits := remoteFixture(t, pin, true)
	if _, err := r.Box(); err != nil {
		t.Skipf("no script sandbox: %v", err)
	}
	out := r.Run(context.Background(), RunSkillScriptInput{Skill: "remote", Script: "fetched"})
	require.Equal(t, "", out.Error, "%+v", out)
	assert.Equal(t, "from storage\n", out.Stdout)
	assert.EqualValues(t, 1, hits.Load())
	again := r.Run(context.Background(), RunSkillScriptInput{Skill: "remote", Script: "fetched"})
	require.Equal(t, "", again.Error, "%+v", again)
	assert.EqualValues(t, 1, hits.Load(), "the second run uses the cache")

	bad, _ := remoteFixture(t, strings.Repeat("0", 64), true)
	out = bad.Run(context.Background(), RunSkillScriptInput{Skill: "remote", Script: "fetched"})
	assert.Contains(t, out.Error, "doesn't match the SHA-256")

	off, offHits := remoteFixture(t, pin, false)
	out = off.Run(context.Background(), RunSkillScriptInput{Skill: "remote", Script: "fetched"})
	assert.Contains(t, out.Error, "web access is off")
	assert.Zero(t, offHits.Load())
}

func TestActivateListsAssets(t *testing.T) {
	r, _ := remoteFixture(t, hexSHA256([]byte(remoteSource)), false)
	p := config.DefaultConfig().Skills.Policy
	out := runTool(t, toolOf(t)(NewActivateSkillTool(r.provider, &p)), map[string]any{"skill_name": "remote"})
	assets, _ := out["assets"].([]any)
	require.Len(t, assets, 2, "%v", out)
	guide := assets[0].(map[string]any)
	assert.Equal(t, "document_text", guide["category"])
	assert.Equal(t, "https://storage.googleapis.com/assets/guide.pdf", guide["url"])
	assert.Equal(t, "application/pdf", guide["mime_type"])
	assert.Equal(t, "How we write", guide["summary"])
	assert.Equal(t, "Use short sentences.", guide["text"], "auto_inject_context gives the text")
	assert.Equal(t, "Remember the tabs.", assets[1].(map[string]any)["content"])
}
