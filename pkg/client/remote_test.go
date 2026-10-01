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

package client

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/apps/service/servicetest"
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gone attaches to a service and then stops it: every call after fails.
// It returns the client and the warnings it gave.
func gone(t *testing.T) (*Remote, *[]string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
	s := servicetest.New(func(ctx context.Context, dir string) (*engine.Workspace, error) {
		cfg := config.DefaultConfig()
		cfg.Tools.WorkspaceDir = dir
		cfg.Session.StorageDir = t.TempDir()
		return engine.Open(ctx, cfg, engine.Options{Model: runtime.NewMockLLM("gemini-3.8-flash")})
	})
	srv := httptest.NewServer(s.Handler())
	warnings := &[]string{}
	r, err := AttachHTTP(context.Background(), http.DefaultClient, srv.URL, t.TempDir(), func(w string) { *warnings = append(*warnings, w) })
	require.NoError(t, err)
	srv.Close()
	s.Close()
	r.ranIn("session-1") // so the process and task calls ask the service
	return r, warnings
}

// With the service gone, the calls that return an error return one.
func TestRemoteCallsFailWithoutTheService(t *testing.T) {
	r, _ := gone(t)
	ctx := context.Background()
	for name, call := range map[string]func() error{
		"Run": func() error {
			_, err := r.Run(ctx, "s", api.Turn{Text: "hi", Timeout: time.Minute}, func(api.Event) {})
			return err
		},
		"Steer":                func() error { return r.Steer(ctx, "s", "x") },
		"OpenSession":          func() error { _, _, err := r.OpenSession("", false); return err },
		"ListSessions":         func() error { _, err := r.ListSessions(true); return err },
		"NewSession":           func() error { _, err := r.NewSession(); return err },
		"LoadSession":          func() error { _, _, err := r.LoadSession("x"); return err },
		"SaveSnapshot":         func() error { _, err := r.SaveSnapshot("x", false); return err },
		"RenameSession":        func() error { _, err := r.RenameSession("x"); return err },
		"ForkSession":          func() error { _, err := r.ForkSession(ctx, 0); return err },
		"ExportSession":        func() error { _, err := r.ExportSession("x"); return err },
		"MoveSession":          func() error { _, err := r.MoveSession("x"); return err },
		"SetAgent":             func() error { _, err := r.SetAgent(ctx, "qa"); return err },
		"SetModel":             func() error { _, err := r.SetModel(ctx, "m"); return err },
		"PinModel":             func() error { _, err := r.PinModel(ctx, "qa", "m"); return err },
		"Unpin":                func() error { _, err := r.Unpin(ctx, "qa"); return err },
		"ModelSettings":        func() error { _, err := r.ModelSettings("m"); return err },
		"UpdateModelSettings":  func() error { _, err := r.UpdateModelSettings("m", true, nil); return err },
		"Set":                  func() error { _, err := r.Set(ctx, "effort", "low"); return err },
		"AddPermissionRule":    func() error { _, err := r.AddPermissionRule("ask", "shell(x)", api.ScopeSession); return err },
		"RemovePermissionRule": func() error { _, err := r.RemovePermissionRule("shell(x)", api.ScopeSession); return err },
		"SetPermissionMode":    func() error { _, err := r.SetPermissionMode("plan"); return err },
		"ListEnvs":             func() error { _, err := r.ListEnvs(); return err },
		"RemoveEnv":            func() error { return r.RemoveEnv("x") },
		"PruneEnvs":            func() error { _, err := r.PruneEnvs(); return err },
		"SessionUsage":         func() error { _, err := r.SessionUsage(); return err },
		"Context":              func() error { _, err := r.Context(true); return err },
		"Compact":              func() error { _, err := r.Compact(ctx, ""); return err },
		"SetGoal":              func() error { _, err := r.SetGoal("x"); return err },
		"Goal":                 func() error { _, err := r.Goal(); return err },
		"ClearGoal":            func() error { return r.ClearGoal() },
		"RewindPoints":         func() error { _, err := r.RewindPoints(); return err },
		"Rewind":               func() error { _, err := r.Rewind(ctx, 0, api.RewindBoth, false); return err },
		"ReloadMemory":         func() error { _, err := r.ReloadMemory(ctx); return err },
		"AddMemory":            func() error { _, err := r.AddMemory(ctx, "x"); return err },
		"SetLocale":            func() error { _, err := r.SetLocale(ctx, "fr"); return err },
		"Undo":                 func() error { _, err := r.Undo(false); return err },
		"GitDiff":              func() error { _, err := r.GitDiff(ctx, false); return err },
		"GitStatus":            func() error { _, err := r.GitStatus(ctx); return err },
		"LoadImage":            func() error { _, err := r.LoadImage("a.png"); return err },
		"AddImage":             func() error { _, err := r.AddImage("a.png", nil); return err },
		"LoadAttachments":      func() error { _, err := r.LoadAttachments([]string{"a.png"}, "", nil); return err },
		"SearchProvider":       func() error { _, err := r.SearchProvider(); return err },
		"SearchWeb":            func() error { _, err := r.SearchWeb(ctx, "x"); return err },
		"TrustProject":         func() error { return r.TrustProject("h", true) },
		"ListNotes":            func() error { _, err := r.ListNotes(); return err },
		"ForgetNote":           func() error { return r.ForgetNote("x") },
		"Task":                 func() error { _, _, err := r.Task("task-1"); return err },
		"StopTask":             func() error { _, err := r.StopTask("task-1"); return err },
		"AnswerTaskRequest":    func() error { return r.AnswerTaskRequest("r", api.DecisionOnce, "") },
		"WaitAll":              func() error { return r.Processes().WaitAll(ctx) },
		"StartBackground":      func() error { _, err := r.StartBackground(ctx, api.Turn{Text: "x", Timeout: time.Minute}); return err },
		"FollowBackground":     func() error { _, err := r.FollowBackground(ctx, "bg-1", func(api.Event) {}); return err },
	} {
		t.Run(name, func(t *testing.T) {
			assert.Error(t, call())
		})
	}
	set, err := r.Set(ctx, " Effort ", "low")
	assert.Error(t, err)
	assert.Equal(t, "effort", set, "the key as it would have been applied")
}

// With the service gone, the calls that can't return an error tell warn
// and return nothing.
func TestRemoteCallsWarnWithoutTheService(t *testing.T) {
	r, warnings := gone(t)
	for name, call := range map[string]func() any{
		"ActiveSession":       func() any { s, _ := r.ActiveSession(); return s.ID },
		"ListAgents":          func() any { return r.ListAgents() },
		"ActiveAgent":         func() any { return r.ActiveAgent().Name },
		"Model":               func() any { return r.Model().Name },
		"AllModelSettings":    func() any { return r.AllModelSettings() },
		"Settings":            func() any { return r.Settings().Agent },
		"ImagesEnabled":       func() any { return r.ImagesEnabled() },
		"ListCommands":        func() any { return r.ListCommands() },
		"ListPermissionRules": func() any { return r.ListPermissionRules() },
		"ListSkills":          func() any { return r.ListSkills() },
		"SearchSkills":        func() any { return r.SearchSkills("x") },
		"Skill":               func() any { s, _ := r.Skill("x"); return s.Name },
		"ListMCPServers":      func() any { return r.ListMCPServers() },
		"ActiveAgentTools":    func() any { return r.ActiveAgentTools().Tools },
		"MemoryFiles":         func() any { return r.MemoryFiles() },
		"AvailableLocales":    func() any { l, _ := r.AvailableLocales(); return l },
		"SandboxSummary":      func() any { return r.SandboxSummary() },
		"ListCheckpoints":     func() any { return r.ListCheckpoints() },
		"SessionDiff":         func() any { return r.SessionDiff() },
		"ListApprovals":       func() any { return r.ListApprovals() },
		"RevokeApprovals":     func() any { return r.RevokeApprovals("k") },
		"ClearApprovals":      func() any { return r.ClearApprovals() },
		"SearchSession":       func() any { n, _ := r.SearchSession("x"); return n },
		"ProjectSettings":     func() any { return r.ProjectSettings().Hash },
		"ListHooks":           func() any { return r.ListHooks() },
		"ListStyles":          func() any { return r.ListStyles() },
		"Running":             func() any { return r.Processes().Running() },
		"Shutdown":            func() any { r.Processes().Shutdown(); return nil },
		"AuditShell":          func() any { r.AuditShell("ls", 0, errors.New("x")); return nil },
		"ListTasks":           func() any { return r.ListTasks() },
		"PendingTaskRequests": func() any { return r.PendingTaskRequests() },
	} {
		t.Run(name, func(t *testing.T) {
			before := len(*warnings)
			assert.Empty(t, call())
			assert.Greater(t, len(*warnings), before, "warn wasn't told")
		})
	}
	assert.Empty(t, r.TakeProcessNotices("s"), "notices without the service")
}

// The rest of Backend over a live service: sessions, models, skills,
// memory, languages, changes, images, notes and the like come back as
// the local workspace would return them.
func TestRemoteBackendCalls(t *testing.T) {
	create := call("create_file", map[string]any{"path": "made.txt", "content": "hi\n"})
	r := attach(t, func(c *config.Config) {
		c.Blitz.AutoApprove = false
		c.Hooks.PromptSubmit = []config.HookConfig{{Command: "true"}}
		c.Web.Enabled = false
	}, create, text("made"), text("second"))
	ctx := context.Background()

	_, found := r.ActiveSession()
	assert.False(t, found, "a session before any was opened")
	assert.ErrorIs(t, r.ClearGoal(), api.ErrNoActiveSession)

	// A turn whose write is approved for good, through the UI.
	r.SetUI(func(context.Context, api.ApprovalRequest) (api.Decision, error) { return api.DecisionAlways, nil }, nil)
	s, err := r.NewSession()
	require.NoError(t, err)
	_, err = r.Run(ctx, s.ID, api.Turn{Text: "make a file", Timeout: time.Minute}, func(api.Event) {})
	require.NoError(t, err)
	_, err = r.Run(ctx, s.ID, api.Turn{Text: "again"}, func(api.Event) {})
	require.NoError(t, err)

	t.Run("sessions", func(t *testing.T) {
		list, err := r.ListSessions(true)
		require.NoError(t, err)
		assert.Len(t, list, 1)
		loaded, branched, err := r.LoadSession(s.ID)
		require.NoError(t, err)
		assert.False(t, branched)
		assert.Equal(t, s.ID, loaded.ID)
		snap, err := r.SaveSnapshot("snap", false)
		require.NoError(t, err)
		assert.Equal(t, "snap", snap.Snapshot)
		renamed, err := r.RenameSession("Renamed")
		require.NoError(t, err)
		assert.Equal(t, "Renamed", renamed.Title)
		md, err := r.ExportSession(s.ID)
		require.NoError(t, err)
		assert.Contains(t, md, "make a file")
		_, err = r.SessionUsage()
		require.NoError(t, err)
		info, err := r.Context(true)
		require.NoError(t, err)
		assert.NotEmpty(t, info.Parts)
		n, prompt := r.SearchSession("again")
		assert.Positive(t, n)
		assert.NotEmpty(t, prompt)
		g, err := r.SetGoal("tests pass")
		require.NoError(t, err)
		assert.Equal(t, "tests pass", g.Condition)
		g, err = r.Goal()
		require.NoError(t, err)
		assert.Equal(t, "tests pass", g.Condition)
		require.NoError(t, r.ClearGoal())
		_, err = r.Goal()
		assert.ErrorIs(t, err, api.ErrNoGoal)
		compacted, err := r.Compact(ctx, "keep the file names")
		require.NoError(t, err)
		assert.Positive(t, compacted.EventsCompacted)
		forked, err := r.ForkSession(ctx, 0)
		require.NoError(t, err)
		assert.NotEqual(t, s.ID, forked.ID)
		assert.Empty(t, r.TakeProcessNotices(s.ID))
	})

	t.Run("agents and models", func(t *testing.T) {
		a, err := r.SetAgent(ctx, "qa")
		require.NoError(t, err)
		assert.Equal(t, "qa", a.Name)
		assert.Equal(t, "qa", r.ActiveAgent().Name)
		pin, err := r.SetModel(ctx, "gemini/gemini-2.5-pro")
		require.NoError(t, err)
		assert.Empty(t, pin)
		assert.Equal(t, "gemini-2.5-pro", r.Model().Name)
		_, err = r.PinModel(ctx, "qa", "anthropic/claude-haiku-4-5")
		require.NoError(t, err)
		un, err := r.Unpin(ctx, "qa")
		require.NoError(t, err)
		assert.Equal(t, "qa", un.Agent)
		_, err = r.UpdateModelSettings("gpt-5", false, []api.Setting{{Key: "max_tokens", Value: "100"}, {Key: "seed", Value: "7"}})
		require.NoError(t, err)
		ms, err := r.ModelSettings("openai/gpt-5")
		require.NoError(t, err)
		require.NotNil(t, ms.Settings.MaxTokens)
		assert.Equal(t, 100, *ms.Settings.MaxTokens)
		require.NotNil(t, ms.Settings.Seed)
		assert.Equal(t, 7, *ms.Settings.Seed)
		_, err = r.ModelSettings("a=b")
		assert.ErrorIs(t, err, api.ErrBadModelRef)
		assert.Equal(t, "qa", r.Settings().Agent)
	})

	t.Run("skills and tools", func(t *testing.T) {
		skills := r.ListSkills()
		require.NotEmpty(t, skills)
		assert.NotEmpty(t, r.SearchSkills(skills[0].Name))
		sk, ok := r.Skill(skills[0].Name)
		assert.True(t, ok)
		assert.Equal(t, skills[0].Name, sk.Name)
		_, ok = r.Skill("nope")
		assert.False(t, ok, "an unknown skill")
		envs, err := r.ListEnvs()
		require.NoError(t, err)
		assert.Empty(t, envs)
		assert.NoError(t, r.RemoveEnv("nope"))
		pruned, err := r.PruneEnvs()
		require.NoError(t, err)
		assert.Zero(t, pruned.Removed)
		assert.Empty(t, r.ListMCPServers())
		assert.NotEmpty(t, r.ActiveAgentTools().Tools)
		assert.NotEmpty(t, r.ListStyles())
		assert.NotEmpty(t, r.ListHooks(), "the configured hook")
	})

	t.Run("memory, notes and languages", func(t *testing.T) {
		p, err := r.AddMemory(ctx, "Run make test.")
		require.NoError(t, err)
		paths, err := r.ReloadMemory(ctx)
		require.NoError(t, err)
		assert.Contains(t, paths, p)
		assert.NotEmpty(t, r.MemoryFiles())
		notes, err := r.ListNotes()
		require.NoError(t, err)
		assert.Empty(t, notes)
		assert.ErrorIs(t, r.ForgetNote("nope"), api.ErrNoNote)
		locales, _ := r.AvailableLocales()
		assert.NotEmpty(t, locales)
		ch, err := r.SetLocale(ctx, "fr-CA")
		require.NoError(t, err)
		assert.Equal(t, "fr-CA", ch.Tag)
		_, err = r.SetLocale(ctx, "not a language at all")
		assert.ErrorIs(t, err, api.ErrUnknownLocale)
	})

	t.Run("changes", func(t *testing.T) {
		_, _, err := r.LoadSession(s.ID) // not the fork
		require.NoError(t, err)
		require.NotEmpty(t, r.ListCheckpoints())
		assert.Contains(t, r.SessionDiff(), "made.txt")
		approvals := r.ListApprovals()
		require.NotEmpty(t, approvals, "approved for good")
		assert.Equal(t, 1, r.RevokeApprovals(approvals[0].Key))
		assert.Zero(t, r.ClearApprovals(), "none left")
		undone, err := r.Undo(false)
		require.NoError(t, err)
		assert.Equal(t, []string{"made.txt"}, undone.Restored)
		_, err = r.Undo(false)
		assert.ErrorIs(t, err, api.ErrNothingToUndo)
		_, err = r.GitDiff(ctx, true)
		assert.Error(t, err, "not a repository")
		p := r.ProjectSettings()
		assert.Equal(t, api.TrustNone, p.State)
	})

	t.Run("images and search", func(t *testing.T) {
		var buf bytes.Buffer
		require.NoError(t, png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 3))))
		img, err := r.AddImage("paste.png", buf.Bytes())
		require.NoError(t, err)
		assert.Equal(t, 4, img.Width)
		require.NoError(t, os.WriteFile(filepath.Join(r.Dir(), "b.png"), buf.Bytes(), 0o644))
		var warned []string
		imgs, err := r.LoadAttachments(nil, "see @b.png and @missing.png", func(w string) { warned = append(warned, w) })
		require.NoError(t, err)
		assert.Len(t, imgs, 1)
		assert.Len(t, warned, 1, "the missing mention")
		_, err = r.SearchProvider()
		assert.ErrorIs(t, err, api.ErrNoFetch, "web access is off")
		_, err = r.SearchWeb(ctx, "x")
		assert.ErrorIs(t, err, api.ErrNoFetch)
	})
	assert.NoError(t, r.Close())
}
