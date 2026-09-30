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
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var pendingProject = api.ProjectSettings{
	Files: []string{".blitz/settings.toml"}, State: api.TrustNew, Hash: "sha256:a",
	Applied: []api.ProjectItem{{Kind: "deny", Value: "web(*)"}},
	Pending: []api.ProjectItem{{Kind: "hook", Key: "stop", Value: "./stop.sh"}, {Kind: "mcp", Key: "db", Value: "npx db-mcp"}},
	Ignored: []api.ProjectItem{{File: ".blitz/settings.toml", Kind: "setting", Key: "llm.openai.base_url", Reason: "never"}},
}

func TestProjectItemInWords(t *testing.T) {
	cases := []struct {
		it   api.ProjectItem
		want string
	}{
		{api.ProjectItem{Kind: "hook", Key: "stop", Value: "./stop.sh"}, "hook stop runs ./stop.sh"},
		{api.ProjectItem{Kind: "mcp", Key: "db", Value: "npx db-mcp"}, "MCP server db: npx db-mcp"},
		{api.ProjectItem{Kind: "allow", Value: "shell(make test)"}, "allow rule shell(make test)"},
		{api.ProjectItem{Kind: "agent_model", Key: "qa", Value: "openai/gpt-5"}, "agent qa uses openai/gpt-5"},
		{api.ProjectItem{Kind: "skill_scripts", Key: "tidy", File: "skills/tidy"}, "skill tidy runs its scripts (skills/tidy)"},
		{api.ProjectItem{Kind: "worker_limit", Key: "workers.policy.max_turns", Value: "10"}, "workers.policy.max_turns = 10"},
		{api.ProjectItem{Kind: "setting", Key: "ui.theme"}, "ui.theme"},
	}
	for _, c := range cases {
		t.Run(c.want, func(t *testing.T) {
			assert.Equal(t, c.want, ProjectItem(c.it))
		})
	}
}

// fakeTruster records the decision it's given.
type fakeTruster struct {
	dir      string
	decision *bool
	loaded   bool
	err      error
}

func (f *fakeTruster) Dir() string { return f.dir }
func (f *fakeTruster) ProjectSettings() api.ProjectSettings {
	return api.ProjectSettings{Loaded: f.loaded}
}
func (f *fakeTruster) TrustProject(_ string, trusted bool) error {
	if f.err != nil {
		return f.err
	}
	f.decision = &trusted
	return nil
}

func TestDecideTrust(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".blitz"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".blitz", "settings.toml"), []byte("[[hooks.stop]]\ncommand = \"./stop.sh\"\n"), 0o644))
	yes, no := true, false
	cases := []struct {
		name     string
		answers  string
		loaded   bool
		err      error
		decision *bool
		says     []string
	}{
		{name: "trusted, in force", answers: "t\n", loaded: true, decision: &yes, says: []string{"hook stop runs ./stop.sh", "They're in force"}},
		{name: "trusted, when it opens again", answers: "trust\n", decision: &yes, says: []string{"restart blitz"}},
		{name: "declined", answers: "d\n", decision: &no, says: []string{"Declined"}},
		{name: "the files shown first", answers: "s\nt\n", loaded: true, decision: &yes, says: []string{"── .blitz/settings.toml", "[[hooks.stop]]"}},
		{name: "no answer", answers: "", says: []string{"want to"}},
		{name: "changed since", answers: "t\n", err: api.ErrProjectChanged, says: []string{"changed since they were shown"}},
		{name: "another answer asks again", answers: "maybe\nd\n", decision: &no},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fakeTruster{dir: dir, loaded: c.loaded, err: c.err}
			var out bytes.Buffer
			DecideTrust(context.Background(), NewLineReader(strings.NewReader(c.answers), &out), &out, f, pendingProject)
			assert.Equal(t, c.decision, f.decision)
			for _, s := range c.says {
				assert.Contains(t, out.String(), s)
			}
		})
	}
}

func TestShowProject(t *testing.T) {
	var out bytes.Buffer
	ShowProject(&out, pendingProject)
	for _, s := range []string{".blitz/settings.toml", "deny rule web(*)", "hook stop runs ./stop.sh", "Not reviewed yet", "llm.openai.base_url", "never from a project"} {
		assert.Contains(t, out.String(), s)
	}
	out.Reset()
	ShowProject(&out, api.ProjectSettings{State: api.TrustNone})
	assert.Contains(t, out.String(), "No project settings")
}

func TestProjectNotice(t *testing.T) {
	cases := []struct {
		name string
		p    api.ProjectSettings
		want string
	}{
		{"nothing", api.ProjectSettings{}, ""},
		{"applied only", api.ProjectSettings{Applied: pendingProject.Applied}, "1 applied"},
		{"waiting", pendingProject, "2 waiting for your trust"},
		{"loaded", api.ProjectSettings{Applied: pendingProject.Applied, Pending: pendingProject.Pending, Loaded: true}, "1 applied"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ProjectNotice(c.p)
			if c.want == "" {
				assert.Empty(t, got)
			} else {
				assert.Contains(t, got, c.want)
			}
		})
	}
	assert.True(t, NeedsTrustDecision(pendingProject))
	assert.False(t, NeedsTrustDecision(api.ProjectSettings{State: api.TrustDeclined}))
}
