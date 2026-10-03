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
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

// fakeSpeaker says what it was asked, as its "audio".
type fakeSpeaker struct {
	ext  string
	got  []SpeakRequest
	fail error
}

func (f *fakeSpeaker) Model() string { return "gemini/tts" }
func (f *fakeSpeaker) Host() string  { return "generativelanguage.googleapis.com" }
func (f *fakeSpeaker) Ext() string   { return f.ext }
func (f *fakeSpeaker) Speak(_ context.Context, req SpeakRequest) (*Speech, error) {
	f.got = append(f.got, req)
	usage := &genai.GenerateContentResponseUsageMetadata{CandidatesTokenCount: 42}
	if f.fail != nil {
		return &Speech{Usage: usage}, f.fail
	}
	return &Speech{Data: []byte("AUDIO:" + req.Text), MIME: "audio/wav", Seconds: 2.5, Usage: usage}, nil
}

func TestGenerateAudio(t *testing.T) {
	tests := []struct {
		name    string
		files   map[string]string
		args    map[string]any
		cfg     config.AudioConfig
		network bool
		fail    error
		noModel error
		wantOut string
		wantErr string
		check   func(t *testing.T, f *fakeSpeaker, out map[string]any)
	}{
		{name: "text", args: map[string]any{"text": "Hello class.", "path": "audio/hi.wav"}, network: true, wantOut: "audio/hi.wav",
			check: func(t *testing.T, f *fakeSpeaker, out map[string]any) {
				assert.Equal(t, "Hello class.", f.got[0].Text)
				assert.EqualValues(t, 2.5, out["seconds"])
				assert.Equal(t, "gemini/tts", out["model"])
			}},
		{name: "a script file, Markdown read as speech", files: map[string]string{"script.md": "# Week 1\n\n**Ana:** Hi!\nBen: Read [the paper](p.pdf)."},
			args:    map[string]any{"text_path": "script.md", "path": "o.wav", "speakers": []any{map[string]any{"name": "Ana", "voice": "Kore"}, map[string]any{"name": "Ben"}}},
			network: true, wantOut: "o.wav",
			check: func(t *testing.T, f *fakeSpeaker, _ map[string]any) {
				assert.Equal(t, "Week 1\n\nAna: Hi!\nBen: Read the paper.", f.got[0].Text)
				assert.Equal(t, []config.SpeakerConfig{{Name: "Ana", Voice: "Kore"}, {Name: "Ben"}}, f.got[0].Speakers)
			}},
		{name: "the configured voice and speakers", args: map[string]any{"text": "x", "path": "o.wav"}, network: true, wantOut: "o.wav",
			cfg: config.AudioConfig{Voice: "Puck", Speakers: []config.SpeakerConfig{{Name: "A"}, {Name: "B"}}},
			check: func(t *testing.T, f *fakeSpeaker, _ map[string]any) {
				assert.Equal(t, "Puck", f.got[0].Voice)
				assert.Len(t, f.got[0].Speakers, 2)
			}},
		{name: "the extension follows the model", args: map[string]any{"text": "x", "path": "audio/o.mp3"}, network: true, wantOut: "audio/o.wav",
			check: func(t *testing.T, _ *fakeSpeaker, out map[string]any) {
				assert.Contains(t, out["note"], "makes .wav files")
			}},
		{name: "too long", args: map[string]any{"text": strings.Repeat("a", 11), "path": "o.wav"}, cfg: config.AudioConfig{MaxChars: 10}, network: true, wantErr: "the limit is 10"},
		{name: "no text", args: map[string]any{"text": "# ", "path": "o.wav"}, network: true, wantErr: "no text to read"},
		{name: "text and a path", args: map[string]any{"text": "a", "text_path": "b.md", "path": "o.wav"}, network: true, wantErr: "not both"},
		{name: "missing script", args: map[string]any{"text_path": "nope.md", "path": "o.wav"}, network: true, wantErr: "failed to read"},
		{name: "binary script", files: map[string]string{"b.md": "\x00"}, args: map[string]any{"text_path": "b.md", "path": "o.wav"}, network: true, wantErr: "not a text file"},
		{name: "three speakers", args: map[string]any{"text": "x", "path": "o.wav", "speakers": []any{map[string]any{"name": "A"}, map[string]any{"name": "B"}, map[string]any{"name": "C"}}}, network: true, wantErr: "at most two"},
		{name: "network off", args: map[string]any{"text": "x", "path": "o.wav"}, wantErr: "sandbox.allow_network"},
		{name: "exists", files: map[string]string{"o.wav": "old"}, args: map[string]any{"text": "x", "path": "o.wav"}, network: true, wantErr: "already exists"},
		{name: "overwrite", files: map[string]string{"o.wav": "old"}, args: map[string]any{"text": "x", "path": "o.wav", "overwrite": true}, network: true, wantOut: "o.wav"},
		{name: "outside", args: map[string]any{"text": "x", "path": "../o.wav"}, network: true, wantErr: "outside"},
		{name: "a script outside", args: map[string]any{"text_path": "../s.md", "path": "o.wav"}, network: true, wantErr: "outside"},
		{name: "the model fails", args: map[string]any{"text": "x", "path": "o.wav"}, network: true, fail: errors.New("quota"), wantErr: "speech failed: quota"},
		{name: "no speech model", args: map[string]any{"text": "x", "path": "o.wav"}, network: true, noModel: errors.New("no key"), wantErr: "speech isn't available: no key"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ws, dir := newTestWorkspace(t)
			for name, content := range tc.files {
				writeFile(t, filepath.Join(dir, name), content)
			}
			f := &fakeSpeaker{ext: ".wav", fail: tc.fail}
			var billed []int32
			s := &speech{speechState: speechState{speaker: f, cfg: tc.cfg, billed: func(_ context.Context, _, model string, u *genai.GenerateContentResponseUsageMetadata) {
				assert.Equal(t, "gemini/tts", model)
				billed = append(billed, u.CandidatesTokenCount)
			}}}
			if tc.noModel != nil {
				s = &speech{speechState: speechState{err: tc.noModel, cfg: tc.cfg}}
			}
			out := runTool(t, toolOf(t)(NewGenerateAudioTool(ws, allowAll(), s, tc.network)), tc.args)
			if tc.wantErr != "" {
				assert.Contains(t, errOf(out), tc.wantErr)
				if tc.fail != nil {
					assert.Equal(t, []int32{42}, billed, "a failed call's usage is still billed")
				}
				return
			}
			require.Empty(t, errOf(out))
			assert.Equal(t, tc.wantOut, out["path"])
			data, err := os.ReadFile(filepath.Join(dir, tc.wantOut))
			require.NoError(t, err)
			assert.True(t, strings.HasPrefix(string(data), "AUDIO:"))
			assert.Equal(t, []int32{42}, billed)
			if tc.check != nil {
				tc.check(t, f, out)
			}
		})
	}
}

func TestGenerateAudioApproval(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	cp := NewCheckpoints(ws, 0)
	f := &fakeSpeaker{ext: ".wav"}

	h, reqs := approverHooks(false)
	out := runTool(t, toolOf(t)(NewGenerateAudioTool(ws, h, &speech{speechState: speechState{speaker: f}}, true)),
		map[string]any{"text": "Ana: Hi\nBen: Yo", "path": "a.wav", "speakers": []any{map[string]any{"name": "Ana"}, map[string]any{"name": "Ben"}}})
	assert.NotEmpty(t, errOf(out))
	require.Len(t, *reqs, 1)
	req := (*reqs)[0]
	assert.Equal(t, api.ActionNetwork, req.Kind)
	assert.Equal(t, "audio:gemini/tts", req.Key)
	assert.Equal(t, []string{"generativelanguage.googleapis.com"}, req.Targets)
	assert.Equal(t, "Speak 15 characters with gemini/tts (Ana and Ben) → a.wav", req.Detail)
	assert.Empty(t, f.got, "nothing is sent before approval")

	cp.Begin("turn")
	h, _ = approverHooks(true)
	out = runTool(t, toolOf(t)(NewGenerateAudioTool(ws, h, &speech{speechState: speechState{speaker: f}}, true)), map[string]any{"text": "x", "path": "a.wav"})
	require.Empty(t, errOf(out))
	assert.FileExists(t, filepath.Join(dir, "a.wav"))
	_, err := cp.Undo(false)
	require.NoError(t, err)
	assert.NoFileExists(t, filepath.Join(dir, "a.wav"), "undone with the turn")
}

// generate_audio is always registered; agents have it only while there's
// a speech model, which can come and go as the settings change.
func TestGenerateAudioRegistered(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	r, err := NewRegistry(cfg, nil, nil)
	require.NoError(t, err)
	defer r.Close()
	names := func() []string {
		var out []string
		for _, tl := range r.GetToolsForAgent([]string{"generate_audio", "read_file"}) {
			out = append(out, tl.Name())
		}
		return out
	}
	all := func() bool {
		for _, tl := range r.GetAllTools() {
			if tl.Name() == "generate_audio" {
				return true
			}
		}
		return false
	}
	assert.Equal(t, []string{"read_file"}, names())
	assert.False(t, all())

	r.SetSpeaker(&fakeSpeaker{ext: ".wav"}, nil, config.AudioConfig{}, nil)
	assert.Equal(t, []string{"generate_audio", "read_file"}, names())
	assert.True(t, all())
	assert.NoError(t, r.SpeakerErr())

	r.SetSpeaker(nil, errors.New("no key"), config.AudioConfig{Model: "gemini/x"}, nil)
	assert.Equal(t, []string{"read_file"}, names())
	assert.EqualError(t, r.SpeakerErr(), "no key")

	var bare Registry // built by hand, as some tests do
	bare.SetSpeaker(&fakeSpeaker{}, nil, config.AudioConfig{}, nil)
	assert.NoError(t, bare.SpeakerErr())
}

func TestSpeakable(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"plain", "Hello.", "Hello."},
		{"headings and emphasis", "## Key *ideas*\n\nThe **learning rate** `lr`.", "Key ideas\n\nThe learning rate lr."},
		{"lists and tasks", "- one\n2. two\n- [x] three", "one\ntwo\nthree"},
		{"links and images", "See [the paper](p.pdf) and ![a chart](c.png).", "See the paper and a chart."},
		{"front matter, rules, quotes, HTML", "---\ntitle: x\n---\n> quoted<br>\n\n---\n\nend", "quoted\n\nend"},
		{"code fences dropped, code kept", "```python\nx = 1\n```", "x = 1"},
		{"snake_case kept", "the learning_rate value", "the learning_rate value"},
		{"speakers kept", "Ana: Hi.\nBen: Hello.", "Ana: Hi.\nBen: Hello."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, Speakable(tc.in))
		})
	}
}
