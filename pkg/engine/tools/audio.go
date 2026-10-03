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
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/mdpdf"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
	"google.golang.org/genai"
)

// Speaker reads text aloud with a speech model (runtime.NewSpeaker).
type Speaker interface {
	// Model names the speech model, "provider/model".
	Model() string
	// Host is the API's host, for web permission rules.
	Host() string
	// Ext is the extension of the files it makes: ".wav" or ".mp3".
	Ext() string
	// Speak returns req's text as audio.
	Speak(ctx context.Context, req SpeakRequest) (*Speech, error)
}

// SpeakRequest is text to read aloud: in one voice, or as a conversation
// between Speakers whose lines start "Name: ".
type SpeakRequest struct {
	Text     string
	Voice    string
	Speakers []config.SpeakerConfig
}

// Speech is spoken audio.
type Speech struct {
	Data    []byte
	MIME    string  // audio/wav or audio/mpeg
	Seconds float64 // 0 when unknown
	// Usage is what the model reported, for /cost (nil: none).
	Usage *genai.GenerateContentResponseUsageMetadata
}

// SpeechBilling hears what a speech call used: the session it was for,
// the model and its usage (nil when the provider reports none).
type SpeechBilling func(ctx context.Context, session, model string, usage *genai.GenerateContentResponseUsageMetadata)

// speech is generate_audio's state: the speaker, set once the workspace
// has built it (and again when its settings change), or why it couldn't;
// the [audio] settings; what hears of each call's usage.
type speech struct {
	mu sync.Mutex
	speechState
}

// speechState is speech's state at one moment.
type speechState struct {
	speaker Speaker
	err     error
	cfg     config.AudioConfig
	billed  SpeechBilling
}

func (s *speech) get() speechState {
	if s == nil {
		return speechState{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.speechState
}

// SetSpeaker gives generate_audio its speech model (nil: none), or why it
// can't be used (err), with the [audio] settings and what hears of each
// call's usage. Agents have generate_audio only while there's a speaker.
func (r *Registry) SetSpeaker(s Speaker, err error, cfg config.AudioConfig, billed SpeechBilling) {
	if r.speech == nil {
		return
	}
	r.speech.mu.Lock()
	defer r.speech.mu.Unlock()
	r.speech.speechState = speechState{speaker: s, err: err, cfg: cfg, billed: billed}
}

// SpeakerErr is why the [audio] model can't be used; nil when it can, or
// when there's none.
func (r *Registry) SpeakerErr() error { return r.speech.get().err }

// hidden reports whether a registered tool is left out of agents' lists
// for now: generate_audio, without a speech model.
func (r *Registry) hidden(name string) bool {
	if name != "generate_audio" {
		return false
	}
	return r.speech.get().speaker == nil
}

// GenerateAudioInput defines arguments for generate_audio.
type GenerateAudioInput struct {
	Text      string         `json:"text,omitempty" jsonschema:"The text to read aloud (or text_path)"`
	TextPath  string         `json:"text_path,omitempty" jsonschema:"A workspace file whose text to read aloud, such as a script in Markdown"`
	Path      string         `json:"path" jsonschema:"Where to write the audio, e.g. audio/overview.wav; the extension follows the speech model (.wav or .mp3)"`
	Voice     string         `json:"voice,omitempty" jsonschema:"The voice for one speaker, by the provider's name for it"`
	Speakers  []SpeakerInput `json:"speakers,omitempty" jsonschema:"Two named voices for a conversation; each line of the text starts with a speaker's name and a colon"`
	Overwrite bool           `json:"overwrite,omitempty" jsonschema:"Replace the file if it already exists"`
}

// SpeakerInput is one voice in a conversation.
type SpeakerInput struct {
	Name  string `json:"name" jsonschema:"The speaker's name, as each of their lines starts"`
	Voice string `json:"voice,omitempty" jsonschema:"The provider's voice name"`
}

// GenerateAudioOutput describes the audio written.
type GenerateAudioOutput struct {
	Path    string  `json:"path"`
	Bytes   int     `json:"bytes,omitempty"`
	Seconds float64 `json:"seconds,omitempty"`
	Model   string  `json:"model,omitempty"`
	Note    string  `json:"note,omitempty"`
	Success bool    `json:"success"`
	Error   string  `json:"error,omitempty"`
}

// NewGenerateAudioTool reads text aloud with the [audio] speech model and
// writes the audio into the workspace: an audio overview of notes, a
// two-voice conversation about a paper. One approval covers the call (it
// sends the text to the provider) and the file it writes, which a
// checkpoint can undo.
func NewGenerateAudioTool(ws *Workspace, hooks *Hooks, s *speech, allowNetwork bool) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name: "generate_audio",
			Description: "Read text aloud with a speech model and save it as an audio file, such as an audio overview of notes. " +
				"For a conversation between two hosts, give speakers and start each line with a speaker's name and a colon. Keep it under about ten minutes (9,000 characters); Markdown formatting isn't read out.",
		},
		func(ctx agent.Context, input GenerateAudioInput) (GenerateAudioOutput, error) {
			fail := func(msg string) (GenerateAudioOutput, error) {
				return GenerateAudioOutput{Path: input.Path, Error: msg}, nil
			}
			st := s.get()
			speaker, cfg, billed := st.speaker, st.cfg, st.billed
			if speaker == nil {
				why := "no speech model is set ([audio] model)"
				if st.err != nil {
					why = st.err.Error()
				}
				return fail("speech isn't available: " + why)
			}
			maxChars := cfg.MaxChars
			if maxChars <= 0 {
				maxChars = 9_000
			}
			text := input.Text
			if input.TextPath != "" {
				if text != "" {
					return fail("give text or text_path, not both")
				}
				rel, err := ws.Rel(input.TextPath)
				if err != nil {
					return fail(err.Error())
				}
				data, err := ws.ReadFile(rel)
				if err != nil {
					return fail(fmt.Sprintf("failed to read %s: %v", input.TextPath, err))
				}
				if err := mdpdf.CheckText(data); err != nil {
					return fail(fmt.Sprintf("%s: %v", input.TextPath, err))
				}
				text = string(data)
			}
			text = Speakable(text)
			n := utf8.RuneCountInString(text)
			switch {
			case n == 0:
				return fail("there is no text to read")
			case n > maxChars:
				return fail(fmt.Sprintf("the text is %d characters; the limit is %d ([audio] max_chars): split it into parts", n, maxChars))
			case !allowNetwork:
				return fail("network access is disabled (sandbox.allow_network = false)")
			}
			speakers := cfg.Speakers
			if len(input.Speakers) > 0 {
				speakers = nil
				for _, sp := range input.Speakers {
					speakers = append(speakers, config.SpeakerConfig{Name: sp.Name, Voice: sp.Voice})
				}
			}
			if len(speakers) > 2 {
				return fail("at most two speakers")
			}

			out, note := input.Path, ""
			if ext := speaker.Ext(); !strings.EqualFold(path.Ext(out), ext) {
				out = strings.TrimSuffix(out, path.Ext(out)) + ext
				note = fmt.Sprintf("%s makes %s files, so the audio is %s.", speaker.Model(), ext, out)
			}
			rel, err := ws.WritablePath(out)
			if err != nil {
				return fail(err.Error())
			}
			if rel == "." {
				return fail("path must name a file")
			}
			if _, err := ws.Stat(rel); err == nil && !input.Overwrite {
				return fail(fmt.Sprintf("file '%s' already exists; set overwrite=true to replace it", out))
			}

			voices := "one voice"
			if len(speakers) == 2 {
				voices = fmt.Sprintf("%s and %s", speakers[0].Name, speakers[1].Name)
			}
			if err := hooks.Approve(ctx, api.ApprovalRequest{
				Tool: "generate_audio", Kind: api.ActionNetwork,
				Detail: fmt.Sprintf("Speak %d characters with %s (%s) → %s", n, speaker.Model(), voices, rel),
				Key:    "audio:" + speaker.Model(), KeyLabel: "speech with " + speaker.Model(), Targets: []string{speaker.Host()},
			}); err != nil {
				return fail(err.Error())
			}

			sp, err := speaker.Speak(ctx, SpeakRequest{Text: text, Voice: cmp.Or(input.Voice, cfg.Voice), Speakers: speakers})
			if billed != nil && sp != nil {
				billed(context.WithoutCancel(ctx), sessionOf(ctx), speaker.Model(), sp.Usage)
			}
			if err != nil {
				return fail(fmt.Sprintf("speech failed: %v", err))
			}

			unlock, err := ws.lockPaths(ctx, rel)
			if err != nil {
				return fail(err.Error())
			}
			defer unlock()
			if input.Overwrite {
				err = ws.WriteFileAtomic(ctx, rel, sp.Data)
			} else {
				err = ws.CreateExclusive(ctx, rel, sp.Data)
				if errors.Is(err, fs.ErrExist) {
					return fail(fmt.Sprintf("file '%s' already exists; set overwrite=true to replace it", out))
				}
			}
			if err != nil {
				return fail(fmt.Sprintf("failed to write file: %v", err))
			}
			return GenerateAudioOutput{Path: rel, Bytes: len(sp.Data), Seconds: sp.Seconds, Model: speaker.Model(), Note: note, Success: true}, nil
		},
	)
}

var (
	mdFence    = regexp.MustCompile("(?m)^\\s*(```|~~~).*$")
	mdHeading  = regexp.MustCompile(`(?m)^\s{0,3}#{1,6}\s+`)
	mdListItem = regexp.MustCompile(`(?m)^\s*(?:[-*+]|\d+[.)])\s+(?:\[[ xX]\]\s+)?`)
	mdQuote    = regexp.MustCompile(`(?m)^\s*>\s?`)
	mdImage    = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	mdLink     = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	mdEmphasis = regexp.MustCompile(`(\*\*|__|~~|\*|` + "`" + `)`) // not a lone _, as in snake_case
	mdRule     = regexp.MustCompile(`(?m)^\s*([-*_])(\s*[-*_]){2,}\s*$`)
	mdHTML     = regexp.MustCompile(`<[^>\n]+>`)
	blankRun   = regexp.MustCompile(`\n{3,}`)
)

// Speakable is Markdown as it should be read out: without front matter,
// heading and list marks, emphasis, link targets, rules and HTML tags. A
// "Name: line" stays as it is, for a conversation's speakers.
func Speakable(md string) string {
	s := strings.ReplaceAll(md, "\r\n", "\n")
	if rest, ok := strings.CutPrefix(s, "---\n"); ok {
		if i := strings.Index(rest, "\n---\n"); i >= 0 {
			s = rest[i+5:]
		}
	}
	s = mdFence.ReplaceAllString(s, "")
	s = mdRule.ReplaceAllString(s, "")
	s = mdHeading.ReplaceAllString(s, "")
	s = mdListItem.ReplaceAllString(s, "")
	s = mdQuote.ReplaceAllString(s, "")
	s = mdImage.ReplaceAllString(s, "$1")
	s = mdLink.ReplaceAllString(s, "$1")
	s = mdHTML.ReplaceAllString(s, "")
	s = mdEmphasis.ReplaceAllString(s, "")
	s = blankRun.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}
