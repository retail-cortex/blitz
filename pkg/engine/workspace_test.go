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
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/agents"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/engine/session"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// openTest opens a workspace around a mock model, isolated from the real
// home directory.
func openTest(t *testing.T) *Workspace {
	w, _ := openTestWith(t, nil)
	return w
}

// openTestWith is openTest with configuration changes and model replies.
func openTestWith(t *testing.T, mutate func(*config.Config), replies ...*genai.Content) (*Workspace, *runtime.MockLLM) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
	t.Chdir(t.TempDir())
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Session.StorageDir = t.TempDir()
	cfg.Tools.ApprovalsFile = filepath.Join(t.TempDir(), "a.json")
	if mutate != nil {
		mutate(cfg)
	}
	llm := runtime.NewMockLLM("gemini-3.8-flash", replies...)
	w, err := Open(context.Background(), cfg, Options{Model: llm, NewModel: mockModels})
	require.NoError(t, err)
	t.Cleanup(func() { w.Close() })
	return w, llm
}

// mockModels builds a mock named after the reference, without its provider.
func mockModels(_ context.Context, _ *config.Config, ref string) (model.LLM, error) {
	if ref == "broken" {
		return nil, errors.New("no such model")
	}
	_, name := runtime.ParseModelRef(ref, "")
	return runtime.NewMockLLM(name), nil
}

// signInModels builds mock models only once signedIn is set, as a sign-in
// outside Blitz would allow; each build is a new generation of the model,
// named "<name>-v<generation>".
type signInModels struct {
	signedIn   atomic.Bool
	generation atomic.Int32
}

func (s *signInModels) build(_ context.Context, _ *config.Config, ref string) (model.LLM, error) {
	if !s.signedIn.Load() {
		return nil, errors.New("no credentials: sign in first")
	}
	_, name := runtime.ParseModelRef(ref, "")
	if name == "" {
		name = "configured"
	}
	return runtime.NewMockLLM(fmt.Sprintf("%s-v%d", name, s.generation.Load()), genai.NewContentFromText("done", genai.RoleModel)), nil
}

// isolatedConfig is a configuration kept away from the real home directory.
func isolatedConfig(t *testing.T) *config.Config {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
	t.Chdir(t.TempDir())
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Session.StorageDir = t.TempDir()
	cfg.Tools.ApprovalsFile = filepath.Join(t.TempDir(), "a.json")
	return cfg
}

// A model that couldn't be built (no sign-in yet) is built again when
// asked about, or before a turn, once the sign-in exists.
func TestRetryModelAfterSignIn(t *testing.T) {
	for _, tc := range []struct {
		name  string
		retry func(t *testing.T, w *Workspace)
	}{
		{"when asked", func(t *testing.T, w *Workspace) { require.NoError(t, w.RetryModel(context.Background())) }},
		{"before a turn", func(t *testing.T, w *Workspace) {
			sess, err := w.NewSession()
			require.NoError(t, err)
			res, err := w.Run(context.Background(), sess.ID, api.Turn{Text: "hi"}, func(api.Event) {})
			require.NoError(t, err)
			assert.Equal(t, "done", res.Output)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			models := &signInModels{}
			w, err := Open(context.Background(), isolatedConfig(t), Options{NewModel: models.build})
			require.NoError(t, err)
			t.Cleanup(func() { w.Close() })
			require.Error(t, w.ModelErr(), "the model was built without a sign-in")
			assert.Error(t, w.RetryModel(context.Background()), "still no sign-in")

			models.signedIn.Store(true)
			tc.retry(t, w)
			assert.NoError(t, w.ModelErr())
			assert.Equal(t, "configured-v0", w.Model().Name)
		})
	}
}

// A model that can't be built doesn't answer: a turn fails with why,
// both at open and after a settings change that breaks a working model
// (not the old model, which the settings no longer describe).
func TestUnavailableModelFailsTurns(t *testing.T) {
	models := &signInModels{}
	cfg := isolatedConfig(t)
	w, err := Open(context.Background(), cfg, Options{NewModel: models.build})
	require.NoError(t, err)
	t.Cleanup(func() { w.Close() })
	assert.Equal(t, cfg.ModelName(), w.Model().Name, "the configured model's name")

	turn := func() (api.TurnResult, error) {
		sess, err := w.NewSession()
		require.NoError(t, err)
		return w.Run(context.Background(), sess.ID, api.Turn{Text: "hi"}, func(api.Event) {})
	}
	res, err := turn()
	require.Error(t, err)
	assert.ErrorContains(t, err, "the model isn't available")
	assert.ErrorContains(t, err, "no credentials: sign in first")
	assert.NotEqual(t, "Done.", res.Output)

	// Working, then broken by a settings change.
	models.signedIn.Store(true)
	res, err = turn()
	require.NoError(t, err)
	assert.Equal(t, "done", res.Output)
	models.signedIn.Store(false)
	assert.Error(t, w.ReloadProviders(context.Background(), config.DefaultConfig()))
	_, err = turn()
	assert.ErrorContains(t, err, "the model isn't available", "the old model answered")
}

// A settings change rebuilds agents' pinned models too, not only the
// configured one, so they sign in the new way.
func TestReloadProvidersRebuildsPins(t *testing.T) {
	cfg := isolatedConfig(t)
	cfg.AgentModels = map[string]string{cfg.Blitz.DefaultAgent: "gemini/pinned"}
	models := &signInModels{}
	models.signedIn.Store(true)
	w, err := Open(context.Background(), cfg, Options{NewModel: models.build})
	require.NoError(t, err)
	t.Cleanup(func() { w.Close() })
	name, pinned := w.engine.AgentModel(cfg.Blitz.DefaultAgent)
	require.True(t, pinned)
	assert.Equal(t, "pinned-v0", name)

	models.generation.Store(1)
	reloaded := config.DefaultConfig()
	require.NoError(t, w.ReloadProviders(context.Background(), reloaded))
	name, _ = w.engine.AgentModel(cfg.Blitz.DefaultAgent)
	assert.Equal(t, "pinned-v1", name, "the pin kept its old model")
}

// savedConfig reads back the config file that operations save to.
func savedConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load(filepath.Join(os.Getenv("HOME"), ".blitz"))
	require.NoError(t, err)
	return cfg
}

func isResumeError(err error) bool {
	var re *api.ResumeError
	return errors.As(err, &re)
}

func writePNG(t *testing.T, path string) {
	t.Helper()
	var buf bytes.Buffer
	png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 12, 8)))
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o644))
}

func TestOpenSessionSetsAuditContextAndMapsResumeErrors(t *testing.T) {
	w := openTest(t)
	_, _, err := w.OpenSession("", true)
	assert.True(t, isResumeError(err), "--continue with no sessions: %v", err)
	rec, resumed, err := w.OpenSession("", false)
	require.NoError(t, err, "new session: %+v %v", rec, resumed)
	require.False(t, resumed, "new session: %+v %v %v", rec, resumed, err)
	require.Equal(t, w.Engine().ActiveAgent(), rec.Agent, "new session: %+v %v %v", rec, resumed, err)
	got, resumed, err := w.OpenSession(rec.ID, false)
	assert.NoError(t, err, "resume by id: %+v %v", got, resumed)
	assert.True(t, resumed, "resume by id: %+v %v %v", got, resumed, err)
	assert.Equal(t, rec.ID, got.ID, "resume by id: %+v %v %v", got, resumed, err)
}

func TestSelectSession(t *testing.T) {
	st, err := session.NewStorage(t.TempDir())
	require.NoError(t, err)
	_, _, continueErr := selectSession(st, "", true, "t", "a")
	assert.True(t, isResumeError(continueErr), "--continue with no sessions: %v", continueErr)
	first, resumed, _ := selectSession(st, "", false, "first", "a")
	assert.False(t, resumed, "new session reported as resumed")
	st.AddMessage("user", "hello")
	latest, resumed, err := selectSession(st, "latest", false, "", "")
	assert.NoError(t, err, "resume latest: %+v %v", latest, resumed)
	assert.True(t, resumed, "resume latest: %+v %v %v", latest, resumed, err)
	assert.Equal(t, first.ID, latest.ID, "resume latest: %+v %v %v", latest, resumed, err)
	assert.Len(t, latest.Messages, 1, "resume latest: %+v %v %v", latest, resumed, err)
	byID, _, err := selectSession(st, first.ID, false, "", "")
	assert.NoError(t, err, "resume by id")
	assert.Equal(t, first.ID, byID.ID, "resume by id: %v", err)
	_, _, unknownErr := selectSession(st, "no-such-session", false, "", "")
	assert.True(t, isResumeError(unknownErr), "an unknown id: %v", unknownErr)
	// --resume <name> starts a new session from the snapshot.
	snap, err := st.Snapshot(first.ID, "greeting", false)
	require.NoError(t, err)
	branch, resumed, err := selectSession(st, "greeting", false, "", "")
	assert.NoError(t, err, "resume by name: %+v %v", branch, resumed)
	assert.True(t, resumed, "resume by name: %+v %v %v", branch, resumed, err)
	assert.NotEqual(t, first.ID, branch.ID, "resume by name: %+v %v %v", branch, resumed, err)
	assert.NotEqual(t, snap.ID, branch.ID, "resume by name: %+v %v %v", branch, resumed, err)
	assert.Equal(t, snap.ID, branch.From, "resume by name: %+v %v %v", branch, resumed, err)
	assert.Len(t, branch.Messages, 1, "resume by name: %+v %v %v", branch, resumed, err)
	// --continue skips snapshots even when one is the newest session.
	time.Sleep(10 * time.Millisecond)
	_, snapErr := st.Snapshot(branch.ID, "newest", false)
	require.NoError(t, snapErr)
	cont, _, err := selectSession(st, "", true, "", "")
	assert.NoError(t, err, "--continue picked %s, want %s", cont.ID, branch.ID)
	assert.Equal(t, branch.ID, cont.ID, "--continue picked %s, want %s: %v", cont.ID, branch.ID, err)
}

func TestModelErrorSummary(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.LLM.Gemini.APIKey = "AIzaSySECRETSECRETSECRETSECRETSECRET123"
	err := errors.New(`api key is required. ClientConfig: &genai.ClientConfig{APIKey:"AIzaSySECRETSECRETSECRETSECRETSECRET123"}` + "\nmore")
	got := ModelErrorSummary(err, cfg)
	assert.Equal(t, "api key is required", got, "summary = %q", got)
	leak := ModelErrorSummary(errors.New("bad key AIzaSySECRETSECRETSECRETSECRETSECRET123"), cfg)
	assert.NotContains(t, leak, "SECRET", "key leaked: %q", leak)

	// A quota error is kept whole, on one line, whatever wraps it.
	quota := fmt.Errorf("anthropic: %w", &runtime.QuotaError{Message: "Quota exceeded for aiplatform.googleapis.com/x.\nPlease submit a quota increase request."})
	got = ModelErrorSummary(quota, cfg)
	assert.Contains(t, got, "quota exceeded: Quota exceeded for aiplatform.googleapis.com/x. Please submit a quota increase request.", "summary = %q", got)
	assert.NotContains(t, got, "\n")
}

func TestSelectSessionScopedToWorkspace(t *testing.T) {
	st, _ := session.NewStorage(t.TempDir())
	st.SetWorkspace("/proj/one")
	one, _, _ := selectSession(st, "", false, "one", "a")
	st.SetWorkspace("/proj/two")
	two, _, _ := selectSession(st, "", false, "two", "a") // newest overall

	st.SetWorkspace("/proj/one")
	got, resumed, err := selectSession(st, "", true, "", "")
	assert.NoError(t, err, "--continue in /proj/one picked %v (%v), want %s", got, err, one.ID)
	assert.True(t, resumed, "--continue in /proj/one picked %v (%v), want %s", got, err, one.ID)
	assert.Equal(t, one.ID, got.ID, "--continue in /proj/one picked %v (%v), want %s", got, err, one.ID)
	// Explicit IDs still work across workspaces.
	got, _, err = selectSession(st, two.ID, false, "", "")
	assert.NoError(t, err, "explicit resume across workspaces: %+v", got)
	assert.Equal(t, two.ID, got.ID, "explicit resume across workspaces: %+v %v", got, err)
	assert.Equal(t, "/proj/two", got.Workspace, "explicit resume across workspaces: %+v %v", got, err)
	st.SetWorkspace("/proj/three")
	_, _, err = selectSession(st, "latest", false, "", "")
	assert.True(t, isResumeError(err), "empty workspace should be a usage error naming it: %v", err)
	assert.Contains(t, err.Error(), "/proj/three", "empty workspace should be a usage error naming it: %v", err)
}

func TestAgentModelRefsPrecedence(t *testing.T) {
	dir := t.TempDir()
	for name, model := range map[string]string{"alpha": "anthropic/claude-haiku-4-5", "beta": "openai/gpt-5"} {
		os.WriteFile(filepath.Join(dir, name+".md"), []byte("---\nname: "+name+"\ndisplay_name: "+name+"\ndescription: d\ntools: []\ndefault_model: "+model+"\n---\nprompt\n"), 0o600)
	}
	reg, _ := agents.NewRegistry()
	require.NoError(t, reg.LoadExternalAgents(dir))
	cfg := config.DefaultConfig()
	cfg.AgentModels = map[string]string{"alpha": "gemini-3.8-flash", "ghost": "x"}
	var warnings []string
	refs := agentModelRefs(cfg, reg, func(s string) { warnings = append(warnings, s) })
	require.Equal(t, "gemini-3.8-flash", refs["alpha"], "refs = %v (config pin must win over default_model)", refs)
	require.Equal(t, "openai/gpt-5", refs["beta"], "refs = %v (config pin must win over default_model)", refs)
	require.Len(t, refs, 2, "refs = %v (config pin must win over default_model)", refs)
	require.Len(t, warnings, 1, "warnings = %v", warnings)
	require.Contains(t, warnings[0], "ghost", "warnings = %v", warnings)
}

func TestLoadAttachments(t *testing.T) {
	e := openTest(t)
	ws := e.Tools().Workspace().Dir()
	writePNG(t, filepath.Join(ws, "a.png"))
	writePNG(t, filepath.Join(ws, "b.png")) // same bytes as a.png: deduplicated

	var warnings []string
	warn := func(s string) { warnings = append(warnings, s) }
	got, err := e.LoadAttachments([]string{"a.png"}, "diff mentions @b.png and @gone.png", warn)
	require.NoError(t, err, "got %d images,", len(got))
	require.Len(t, got, 1, "got %d images, %v", len(got), err)
	assert.Len(t, warnings, 1, "a bad mention should warn, not fail: %q", warnings)
	assert.Contains(t, warnings[0], "gone.png", "a bad mention should warn, not fail: %q", warnings)
	_, err = e.LoadAttachments([]string{"missing.png"}, "", warn)
	assert.Error(t, err, "a bad image path must fail")
	assert.Contains(t, err.Error(), "missing.png", "a bad image path must fail: %v", err)
}

func TestSetupLocaleWarnsAndFallsBack(t *testing.T) {
	defer i18n.SetCurrent(nil)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{"), 0o600)
	cfg := config.DefaultConfig()
	cfg.UI.Locale = "not a language!"
	cfg.UI.LocalesDir = dir
	var warnings []string
	SetupLocale(cfg, func(s string) { warnings = append(warnings, s) })
	assert.Len(t, warnings, 2, "warnings = %q", warnings)
	assert.Contains(t, warnings[0], "bad.json", "warnings = %q", warnings)
	assert.Contains(t, warnings[1], "not a language", "warnings = %q", warnings)
	assert.Equal(t, "en-US", i18n.Current().Tag().String(), "fallback locale = %s", i18n.Current().Tag())
}

// A session's usage survives the workspace closing: reopened (a restart,
// --resume), /cost and /context go on from where they were, and new calls
// add to it.
func TestUsageSurvivesARestart(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Session.StorageDir = t.TempDir()
	cfg.Tools.ApprovalsFile = filepath.Join(t.TempDir(), "a.json")
	open := func(reply string, prompt int32) (*Workspace, *runtime.MockLLM) {
		llm := runtime.NewMockLLM("gemini-3.8-flash", genai.NewContentFromText(reply, genai.RoleModel))
		llm.Usage = &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: prompt, CandidatesTokenCount: 50}
		c := *cfg
		w, err := Open(context.Background(), &c, Options{Model: llm, NewModel: mockModels})
		require.NoError(t, err)
		return w, llm
	}
	ctx := context.Background()

	w, _ := open("first", 1000)
	sid := newSession(t, w).ID
	_, err := w.Run(ctx, sid, api.Turn{Text: "one"}, ignore)
	require.NoError(t, err)
	before := w.engine.Usage(sid)
	require.Equal(t, 1, before.Calls)
	require.NoError(t, w.Close())

	w2, _ := open("second", 1500)
	defer w2.Close()
	got := w2.engine.Usage(sid)
	assert.Equal(t, before, got, "usage after reopening")
	_, _, err = w2.OpenSession(sid, false) // --resume
	require.NoError(t, err)
	info, err := w2.Context()
	require.NoError(t, err)
	assert.Equal(t, int64(1000), info.Tokens, "/context reports the last prompt, not 0")

	_, err = w2.Run(ctx, sid, api.Turn{Text: "two"}, ignore)
	require.NoError(t, err)
	after := w2.engine.Usage(sid)
	assert.Equal(t, 2, after.Calls, "new calls add to the saved usage")
	assert.Equal(t, int64(2500), after.Input)
	assert.Equal(t, int64(1500), after.LastPrompt)
	assert.InDelta(t, before.CostUSD*2.5, after.CostUSD, 0.01, "cost goes on from the saved one")
}

// A turn's messages go to the session it runs in: after the service
// restarts (the storage has no active session) and when another session
// is active. Seen 2026-09-28: a conversation continued after a restart was
// saved to the model's history but not its transcript.
func TestTranscriptFollowsTheTurnsSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Session.StorageDir = t.TempDir()
	cfg.Tools.ApprovalsFile = filepath.Join(t.TempDir(), "a.json")
	open := func(replies ...string) *Workspace {
		var cs []*genai.Content
		for _, r := range replies {
			cs = append(cs, genai.NewContentFromText(r, genai.RoleModel))
		}
		c := *cfg
		w, err := Open(context.Background(), &c, Options{Model: runtime.NewMockLLM("gemini-3.8-flash", cs...), NewModel: mockModels})
		require.NoError(t, err)
		return w
	}
	ctx := context.Background()

	w := open()
	sid := newSession(t, w).ID // the desktop shows this new chat
	require.NoError(t, w.Close())

	w2 := open("the tests prove little", "they don't cover errors") // the service restarted
	defer w2.Close()
	_, err := w2.Run(ctx, sid, api.Turn{Text: "Run the tests"}, ignore)
	require.NoError(t, err)
	other := newSession(t, w2).ID // another chat is now active
	_, err = w2.Run(ctx, sid, api.Turn{Text: "What do they prove?"}, ignore)
	require.NoError(t, err)

	rec, err := w2.storage.Load(sid)
	require.NoError(t, err)
	var got []string
	for _, m := range rec.Messages {
		got = append(got, m.Role+": "+m.Content)
	}
	assert.Equal(t, []string{"user: Run the tests", "model: the tests prove little", "user: What do they prove?", "model: they don't cover errors"}, got)
	assert.Equal(t, "Run the tests", rec.Title)
	otherRec, err := w2.storage.Load(other)
	require.NoError(t, err)
	assert.Empty(t, otherRec.Messages, "messages went to the active session")
}
