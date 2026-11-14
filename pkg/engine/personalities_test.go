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
	"errors"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
)

// The built-ins first (default leading), then the organization's, then the
// person's; a person's file of a built-in's name is theirs; every built-in
// has a description, a Gemini voice and text (PER-10, PER-11, PER-12).
func TestListPersonalities(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	org := t.TempDir()
	writePersonality(t, filepath.Join(org, "personalities", "house.md"), "---\ndescription: The house style\n---\nSound like us.\n")
	writePersonality(t, filepath.Join(PersonalitiesDir(), "pirate.md"), "---\ndescription: Arr\nvoice: Fenrir\n---\nTalk like a pirate.\n")
	writePersonality(t, filepath.Join(PersonalitiesDir(), "friendly.md"), "Mine, warmer.\n")
	writePersonality(t, filepath.Join(PersonalitiesDir(), "Bad Name.md"), "skipped\n")

	list := ListPersonalities(config.OrgLayer{Defaults: org})
	var names, sources []string
	for _, p := range list {
		names, sources = append(names, p.Name), append(sources, p.Source)
		assert.Empty(t, p.Text, "lists leave the text out")
	}
	assert.Equal(t, []string{"default", "calm", "cheerful", "direct", "mentor", "reviewer", "house", "friendly", "pirate"}, names)
	assert.Equal(t, []string{PersonalityBuiltIn, PersonalityBuiltIn, PersonalityBuiltIn, PersonalityBuiltIn, PersonalityBuiltIn, PersonalityBuiltIn, PersonalityOrg, PersonalityOwn, PersonalityOwn}, sources)

	for _, name := range []string{"friendly", "direct", "mentor", "cheerful", "calm", "reviewer"} {
		t.Run(name, func(t *testing.T) {
			p, err := GetPersonality(config.OrgLayer{}, name)
			require.NoError(t, err)
			if name == "friendly" {
				assert.Equal(t, PersonalityOwn, p.Source)
				assert.Equal(t, "Mine, warmer.", p.Text)
				return
			}
			assert.NotEmpty(t, p.Description)
			assert.Contains(t, config.GeminiVoices, p.Voice)
			assert.NotEmpty(t, p.Text)
			assert.LessOrEqual(t, len(p.Text), MaxPersonalityText)
		})
	}
	p, err := GetPersonality(config.OrgLayer{}, " Pirate ")
	require.NoError(t, err)
	assert.Equal(t, api.Personality{Name: "pirate", Description: "Arr", Voice: "Fenrir", Source: PersonalityOwn, Text: "Talk like a pirate."}, p)
	_, err = GetPersonality(config.OrgLayer{}, "nope")
	var invalid *api.InvalidSettingError
	assert.ErrorAs(t, err, &invalid)
}

// What can be saved as the person's own, and why not (PER-04, PER-31).
func TestCheckPersonality(t *testing.T) {
	good := api.Personality{Name: "my-voice2", Text: "Be kind.", Voice: "Puck", Description: "One line"}
	for name, tc := range map[string]struct {
		change func(*api.Personality)
		want   string
	}{
		"good":        {func(*api.Personality) {}, ""},
		"bad name":    {func(p *api.Personality) { p.Name = "My Voice" }, "lower-case"},
		"long name":   {func(p *api.Personality) { p.Name = strings.Repeat("a", 41) }, "lower-case"},
		"default":     {func(p *api.Personality) { p.Name = "default" }, "Blitz's own"},
		"no text":     {func(p *api.Personality) { p.Text = "  " }, "needs its text"},
		"long text":   {func(p *api.Personality) { p.Text = strings.Repeat("é", MaxPersonalityText+1) }, "at most 2000"},
		"bad voice":   {func(p *api.Personality) { p.Voice = "Bob" }, "Gemini's voices"},
		"two lines":   {func(p *api.Personality) { p.Description = "a\nb" }, "one line"},
		"secret":      {func(p *api.Personality) { p.Text = "Be kind.\nAKIAIOSFODNN7EXAMPLE" }, "line 2 looks like a secret"},
		"exact limit": {func(p *api.Personality) { p.Text = strings.Repeat("é", MaxPersonalityText) }, ""},
	} {
		t.Run(name, func(t *testing.T) {
			p := good
			tc.change(&p)
			err := CheckPersonality(p)
			if tc.want == "" {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, tc.want)
			}
		})
	}
}

// The person's own are saved, read back and deleted; a built-in or the
// organization's can't be deleted.
func TestSaveAndDeletePersonality(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	p := api.Personality{Name: "coach", Description: " Upbeat coach ", Voice: "Puck", Text: " Cheer them on. "}
	require.NoError(t, SavePersonality(p))
	got, err := GetPersonality(config.OrgLayer{}, "coach")
	require.NoError(t, err)
	assert.Equal(t, api.Personality{Name: "coach", Description: "Upbeat coach", Voice: "Puck", Source: PersonalityOwn, Text: "Cheer them on."}, got)

	require.NoError(t, SavePersonality(api.Personality{Name: "coach", Text: "Plain now."}), "saving again replaces it")
	got, _ = GetPersonality(config.OrgLayer{}, "coach")
	assert.Equal(t, api.Personality{Name: "coach", Source: PersonalityOwn, Text: "Plain now."}, got)
	data, err := os.ReadFile(filepath.Join(PersonalitiesDir(), "coach.md"))
	require.NoError(t, err)
	assert.Equal(t, "Plain now.\n", string(data), "no frontmatter when there's none to keep")

	var invalid *api.InvalidSettingError
	assert.ErrorAs(t, SavePersonality(api.Personality{Name: "x y", Text: "t"}), &invalid)

	org := t.TempDir()
	writePersonality(t, filepath.Join(org, "personalities", "house.md"), "Sound like us.\n")
	assert.ErrorIs(t, DeletePersonality(config.OrgLayer{Locked: org}, "house"), ErrPersonalityReadOnly)
	assert.ErrorIs(t, DeletePersonality(config.OrgLayer{}, "mentor"), ErrPersonalityReadOnly)
	assert.ErrorAs(t, DeletePersonality(config.OrgLayer{}, "ghost"), &invalid)
	require.NoError(t, DeletePersonality(config.OrgLayer{}, "coach"))
	_, err = GetPersonality(config.OrgLayer{}, "coach")
	assert.Error(t, err)
}

// [ui] personality reaches the root agent framed (PER-03), after the output
// style; Set "personality" switches it from the next turn and lists it as
// active; a name that has gone falls back to default with a warning
// (PER-22).
func TestWorkspacePersonality(t *testing.T) {
	w, llm := openTestWith(t, func(c *config.Config) { c.UI.Personality = "mentor"; c.UI.Style = "concise" }, text("a"), text("b"), text("c"), text("d"))
	ctx := context.Background()
	s, _ := w.NewSession()
	system := func() string {
		_, err := w.Run(ctx, s.ID, api.Turn{Text: "hi"}, func(api.Event) {})
		require.NoError(t, err)
		return systemText(llm.Requests[len(llm.Requests)-1].Config)
	}
	sys := system()
	require.Contains(t, sys, "## Your personality")
	assert.Contains(t, sys, "patient mentor")
	assert.Greater(t, strings.Index(sys, "## Your personality"), strings.Index(sys, "Output style: concise"), "after the output style")
	assert.Equal(t, "mentor", w.Personality().Name)

	_, err := w.Set(ctx, "personality", "Direct")
	require.NoError(t, err)
	sys = system()
	assert.Contains(t, sys, "Lead with the answer")
	assert.NotContains(t, sys, "patient mentor")
	for _, p := range w.ListPersonalities() {
		assert.Equal(t, p.Name == "direct", p.Active, p.Name)
	}

	_, err = w.Set(ctx, "personality", "nope")
	var invalid *api.InvalidSettingError
	assert.ErrorAs(t, err, &invalid)
	assert.Equal(t, "direct", w.Personality().Name, "unchanged")

	_, err = w.Set(ctx, "personality", "default")
	require.NoError(t, err)
	assert.NotContains(t, system(), "## Your personality")

	writePersonality(t, filepath.Join(PersonalitiesDir(), "gone.md"), "Short-lived.\n")
	cfg := *w.settings()
	cfg.UI.Personality = "gone"
	w.ReloadPersonality(ctx, &cfg)
	assert.Contains(t, system(), "Short-lived.")
	require.NoError(t, os.Remove(filepath.Join(PersonalitiesDir(), "gone.md")))
	assert.Equal(t, DefaultPersonality, w.Personality().Name, "a file that's gone is default")
	w.ReloadPersonality(ctx, &cfg)
	assert.NotContains(t, system(), "Short-lived.")
}

// A personality that has gone when the workspace opens is a warning, not
// an error.
func TestPersonalityAtOpen(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Session.StorageDir = t.TempDir()
	cfg.UI.Personality = "ghost"
	var warnings []string
	w, err := Open(context.Background(), cfg, Options{Model: runtime.NewMockLLM("gemini-3.8-flash"), NewModel: mockModels, Warn: func(s string) { warnings = append(warnings, s) }})
	require.NoError(t, err)
	defer w.Close()
	assert.Contains(t, warnings, `ui.personality "ghost": no such personality, so Blitz sounds as it does by default`)
	assert.Equal(t, DefaultPersonality, w.Personality().Name)
}

// Try it answers the fixed question in the text's personality, with the
// frame, from the suggestions model; a text that can't be saved can't be
// tried; a model that fails is the error.
func TestTryPersonality(t *testing.T) {
	w := openTest(t)
	llm := &promptLLM{reply: "Let's look at the import together."}
	var system string
	w.newModel = func(_ context.Context, _ *config.Config, _ string) (model.LLM, error) {
		return systemLLM{llm, &system}, nil
	}
	answer, err := w.TryPersonality(context.Background(), "Be a patient mentor.")
	require.NoError(t, err)
	assert.Equal(t, "Let's look at the import together.", answer)
	assert.Contains(t, system, "## Your personality")
	assert.Contains(t, system, "Be a patient mentor.")
	assert.Equal(t, []string{TryQuestion}, llm.prompts)

	_, err = w.TryPersonality(context.Background(), strings.Repeat("x", MaxPersonalityText+1))
	var invalid *api.InvalidSettingError
	assert.ErrorAs(t, err, &invalid)

	llm.err = errors.New("quota exceeded")
	_, err = w.TryPersonality(context.Background(), "")
	assert.ErrorContains(t, err, "quota exceeded")
	w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) { return nil, errors.New("no key") }
	_, err = w.TryPersonality(context.Background(), "")
	assert.ErrorContains(t, err, "no key")
}

// systemLLM is an LLM that keeps the system instruction it was sent.
type systemLLM struct {
	*promptLLM
	system *string
}

func (s systemLLM) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	*s.system = systemText(req.Config)
	return s.promptLLM.GenerateContent(ctx, req, stream)
}

func writePersonality(t *testing.T, path, text string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(text), 0o644))
}

// What the person said about themselves reaches the root agent whatever
// the personality, and nothing is guessed: with nothing given, gender-
// neutral words (PER-05).
func TestAboutPerson(t *testing.T) {
	w, llm := openTestWith(t, func(c *config.Config) {
		c.Profile = config.ProfileConfig{Name: "Ryan", Gender: "male", Pronouns: "he/him"}
	}, text("a"), text("b"))
	ctx := context.Background()
	s, _ := w.NewSession()
	system := func() string {
		_, err := w.Run(ctx, s.ID, api.Turn{Text: "hi"}, func(api.Event) {})
		require.NoError(t, err)
		return systemText(llm.Requests[len(llm.Requests)-1].Config)
	}
	sys := system()
	assert.Contains(t, sys, "## The person")
	assert.Contains(t, sys, "Their name is Ryan.")
	assert.Contains(t, sys, "Their gender: male.")
	assert.Contains(t, sys, "Their pronouns are he/him")
	assert.Contains(t, sys, "Never guess anyone's gender")

	cfg := *w.settings()
	cfg.Profile = config.ProfileConfig{}
	w.ReloadPersonality(ctx, &cfg)
	sys = system()
	assert.NotContains(t, sys, "Ryan")
	assert.Contains(t, sys, "gender-neutral words")
}
