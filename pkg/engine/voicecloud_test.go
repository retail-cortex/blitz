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
	"fmt"
	"io"
	"iter"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/engine/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeCloud stands for Google Cloud's speech: it notes how each thing
// said was asked for and what it was, and hears what a test says it does.
type fakeCloud struct {
	mu       sync.Mutex
	spoke    []spokeAs
	language string
	hear     *fakeHear
	// hold, when set, keeps each stream's audio back until it's closed.
	hold chan struct{}
}

// spokeAs is one stream of speech: its voice, its tuning, its sentences.
type spokeAs struct {
	voice  string
	tuning config.VoiceTuning
	texts  []string
}

type fakeHear struct {
	events  chan runtime.HearEvent
	mu      sync.Mutex
	sent    int
	flushes int
	closed  bool
}

func (h *fakeHear) Send(pcm []byte) {
	h.mu.Lock()
	h.sent += len(pcm)
	h.mu.Unlock()
}

func (h *fakeHear) Flush() {
	h.mu.Lock()
	h.flushes++
	h.mu.Unlock()
}

func (h *fakeHear) Events() <-chan runtime.HearEvent { return h.events }

func (h *fakeHear) Close() {
	h.mu.Lock()
	h.closed = true
	h.mu.Unlock()
}

func (h *fakeHear) counts() (sent, flushes int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sent, h.flushes
}

// says is the person saying text: begun, heard in part, settled.
func (h *fakeHear) says(text string) {
	h.events <- runtime.HearEvent{Kind: runtime.HearBegin}
	h.events <- runtime.HearEvent{Kind: runtime.HearFinal, Text: text}
	h.events <- runtime.HearEvent{Kind: runtime.HearEnd}
}

// fakeSpeech is one stream: each sentence's audio is its text.
type fakeSpeech struct {
	c   *fakeCloud
	i   int
	ctx context.Context
	in  chan string
}

func (s *fakeSpeech) Say(text string) error {
	s.c.mu.Lock()
	s.c.spoke[s.i].texts = append(s.c.spoke[s.i].texts, text)
	s.c.mu.Unlock()
	s.in <- text
	return nil
}

func (s *fakeSpeech) Finish() error { close(s.in); return nil }

func (s *fakeSpeech) Recv() ([]byte, error) {
	s.c.mu.Lock()
	hold := s.c.hold
	s.c.mu.Unlock()
	if hold != nil {
		select {
		case <-hold:
		case <-s.ctx.Done():
			return nil, s.ctx.Err()
		}
	}
	select {
	case t, ok := <-s.in:
		if !ok {
			return nil, io.EOF
		}
		if len(t)%2 == 1 {
			t += " "
		}
		return []byte(t), nil
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
}

func (s *fakeSpeech) Close() {}

func (c *fakeCloud) said() []spokeAs {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]spokeAs, len(c.spoke))
	for i, s := range c.spoke {
		out[i] = spokeAs{s.voice, s.tuning, append([]string(nil), s.texts...)}
	}
	return out
}

// openCloudVoice opens a workspace whose voice is on with the cloud
// engine, its speech faked and its text model answering with replies
// (streamed in small pieces).
func openCloudVoice(t *testing.T, mutate func(*config.Config), replies ...*genai.Content) (*Workspace, *fakeCloud, *runtime.MockLLM) {
	t.Helper()
	w, _ := openTestWith(t, mutate)
	for k, v := range map[string]string{"voice.enabled": "true", "voice.engine": "cloud"} {
		_, err := config.SetValue(w.cfg.Dir, "", k, v)
		require.NoError(t, err)
	}
	fc := &fakeCloud{hear: &fakeHear{events: make(chan runtime.HearEvent, 16)}}
	w.newCloudVoice = func(context.Context, *config.Config) (*runtime.CloudVoice, error) {
		return runtime.NewCloudVoiceWith(
			func(ctx context.Context, voice string, tn config.VoiceTuning) (runtime.SpeechStream, error) {
				fc.mu.Lock()
				defer fc.mu.Unlock()
				fc.spoke = append(fc.spoke, spokeAs{voice: voice, tuning: tn})
				return &fakeSpeech{c: fc, i: len(fc.spoke) - 1, ctx: ctx, in: make(chan string, 64)}, nil
			},
			func(_ context.Context, language string) (runtime.Listener, error) {
				fc.mu.Lock()
				fc.language = language
				fc.mu.Unlock()
				return fc.hear, nil
			}), nil
	}
	llm := runtime.NewMockLLM("voice-text", replies...)
	llm.Chunk = 5
	w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) { return llm, nil }
	return w, fc, llm
}

// instructionOf is a request's system instruction, as text.
func instructionOf(r *model.LLMRequest) string {
	var b strings.Builder
	if r.Config != nil && r.Config.SystemInstruction != nil {
		for _, p := range r.Config.SystemInstruction.Parts {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

func voiceCall(name string, args map[string]any) *genai.Content {
	return &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: name, Args: args}}}}
}

func voiceText(s string) *genai.Content { return genai.NewContentFromText(s, genai.RoleModel) }

// A conversation over the cloud engine (VOX-70 to VOX-75): the person is
// heard in their language, the agent (the live one's instructions and
// tools, told its words are read aloud and in which locale) answers, and
// its answer is said a sentence at a time in the personality's voice,
// locale, speed and pitch (PER-41); both sides are kept as spoken lines;
// end_conversation ends it once its goodbye is said.
func TestCloudVoiceConversation(t *testing.T) {
	w, fc, llm := openCloudVoice(t, func(c *config.Config) {
		require.NoError(t, SavePersonality(api.Personality{Name: "nagatha", Text: "Poised and unhurried.", Voice: "Aoede", Locale: "en-GB", Speed: 0.9, Pitch: -2}))
		c.UI.Personality = "nagatha"
	},
		voiceCall("run_task", map[string]any{"request": "Run the test suite and fix what fails."}),
		voiceText("On it. I'll **tell** you when it's done."),
		voiceCall("end_conversation", nil),
		voiceText("Goodbye for now."),
	)
	s, err := w.NewSession()
	require.NoError(t, err)
	evs := newVoiceEvents()
	v, err := w.StartVoice(context.Background(), VoiceOptions{SessionID: s.ID, OnEvent: evs.on})
	require.NoError(t, err)
	t.Cleanup(v.Close)
	assert.Equal(t, config.VoiceEngineCloud, v.Engine())
	assert.Equal(t, "voice-text", v.ModelID())
	assert.Equal(t, "Aoede", v.VoiceName())

	require.NoError(t, v.Audio(make([]byte, 640)))
	sent, _ := fc.hear.counts()
	assert.Equal(t, 640, sent, "the microphone goes to Speech-to-Text")

	fc.hear.says("Run the tests please")
	assert.Equal(t, "Run the tests please", evs.next(t, "heard").Text)
	assert.Equal(t, "Run the test suite and fix what fails.", evs.next(t, "run_task").Text)
	assert.Equal(t, "speaking", evs.next(t, "state").State)
	var audio []string
	for len(audio) < 2 {
		audio = append(audio, strings.TrimSpace(string(evs.next(t, "audio").Audio)))
	}
	assert.Equal(t, []string{"On it.", "I'll tell you when it's done."}, audio, "a sentence at a time, without the Markdown")
	assert.Equal(t, "listening", evs.next(t, "state").State)

	spoke := fc.said()
	require.Len(t, spoke, 1)
	assert.Equal(t, "Aoede", spoke[0].voice)
	assert.Equal(t, config.VoiceTuning{Locale: "en-GB", Speed: 0.9, Pitch: -2}, spoke[0].tuning, "the personality's locale, speed and pitch")
	fc.mu.Lock()
	assert.Equal(t, "en-GB", fc.language, "heard in the personality's language, [voice] language unset")
	fc.mu.Unlock()

	instruction := instructionOf(llm.Requests[0])
	var names []string
	for name := range llm.Requests[0].Tools {
		names = append(names, name)
	}
	assert.Contains(t, instruction, "always in the first person")
	assert.Contains(t, instruction, "turned into speech exactly as you write them")
	assert.Contains(t, instruction, "the language of the locale en-GB")
	assert.Contains(t, instruction, "Poised and unhurried.")
	assert.ElementsMatch(t, []string{"run_task", "steer", "stop", "status", "answer", "recent", "end_conversation"}, names, "only the voice's tools")

	rec, err := w.storage.Load(s.ID)
	require.NoError(t, err)
	var spoken []string
	for _, m := range rec.Messages {
		if m.Kind == session.KindSpoken {
			spoken = append(spoken, m.Role+": "+m.Content)
		}
	}
	assert.Equal(t, []string{"user: Run the tests please", "model: On it. I'll **tell** you when it's done."}, spoken, "kept in writing")

	fc.hear.says("That's all, goodbye")
	assert.Equal(t, "goodbye", evs.next(t, "end").Text)
	<-v.Done()
	fc.hear.mu.Lock()
	assert.True(t, fc.hear.closed, "Speech-to-Text is let go")
	fc.hear.mu.Unlock()
}

// Speaking over the voice stops it (VOX-75): the window is told to drop
// what it hasn't played, no audio of the reply follows, what was said so
// far is kept, and the person's new words are answered. Words said one
// after the other are answered together.
func TestCloudVoiceInterrupted(t *testing.T) {
	w, fc, _ := openCloudVoice(t, nil, voiceText("First, a long answer. It goes on. And on."), voiceText("Stopping."))
	fc.hold = make(chan struct{}) // the audio is slow in coming
	s, err := w.NewSession()
	require.NoError(t, err)
	evs := newVoiceEvents()
	v, err := w.StartVoice(context.Background(), VoiceOptions{SessionID: s.ID, OnEvent: evs.on})
	require.NoError(t, err)
	t.Cleanup(v.Close)

	fc.hear.says("Tell me everything")
	assert.Equal(t, "speaking", evs.next(t, "state").State)
	fc.hear.events <- runtime.HearEvent{Kind: runtime.HearBegin}
	fc.hear.events <- runtime.HearEvent{Kind: runtime.HearInterim, Text: "wait"}
	evs.next(t, "interrupted")
	assert.Equal(t, "listening", evs.next(t, "state").State)
	close(fc.hold) // what was held back is never sent: the reply is over
	fc.mu.Lock()
	fc.hold = nil
	fc.mu.Unlock()

	fc.hear.events <- runtime.HearEvent{Kind: runtime.HearFinal, Text: "Wait."}
	fc.hear.events <- runtime.HearEvent{Kind: runtime.HearFinal, Text: "Stop there."}
	assert.Equal(t, "Stopping.", strings.TrimSpace(string(evs.next(t, "audio").Audio)), "only the new reply is heard")
	assert.Equal(t, "listening", evs.next(t, "state").State)
	evs.mu.Lock()
	defer evs.mu.Unlock()
	for _, e := range evs.evs {
		if e.Kind == "audio" {
			assert.NotContains(t, string(e.Audio), "long answer", "the interrupted reply's audio isn't sent")
		}
	}
	rec, err := w.storage.Load(s.ID)
	require.NoError(t, err)
	var spoken []string
	for _, m := range rec.Messages {
		if m.Kind == session.KindSpoken {
			spoken = append(spoken, m.Role+": "+m.Content)
		}
	}
	require.GreaterOrEqual(t, len(spoken), 3)
	assert.Equal(t, "user: Tell me everything", spoken[0])
	assert.Contains(t, spoken[1], "model: First", "what it had said is kept")
	assert.Equal(t, "user: Wait. Stop there.", spoken[2], "said together, answered together")
}

// Hold to talk (VOX-07): the microphone is heard only while held;
// pressing stops the voice, and releasing settles what was said without
// waiting for a pause. In a noisy room only settled words interrupt.
func TestCloudVoiceHoldAndNoise(t *testing.T) {
	w, fc, _ := openCloudVoice(t, nil, voiceText("One. Two. Three."), voiceText("Yes?"))
	_, err := config.SetValue(w.cfg.Dir, "", "voice.listen", "hold")
	require.NoError(t, err)
	_, err = config.SetValue(w.cfg.Dir, "", "voice.noise", "noisy")
	require.NoError(t, err)
	evs := newVoiceEvents()
	v, err := w.StartVoice(context.Background(), VoiceOptions{SessionID: "s", OnEvent: evs.on})
	require.NoError(t, err)
	t.Cleanup(v.Close)

	require.NoError(t, v.Audio(make([]byte, 320)))
	sent, _ := fc.hear.counts()
	assert.Zero(t, sent, "not held: not heard")
	require.NoError(t, v.Activity(true))
	require.NoError(t, v.Audio(make([]byte, 320)))
	fc.hear.events <- runtime.HearEvent{Kind: runtime.HearFinal, Text: "Count to three"}
	time.Sleep(2 * cloudSettle)
	evs.mu.Lock()
	for _, e := range evs.evs {
		assert.NotEqual(t, "said", e.Kind, "still held: not answered yet")
	}
	evs.mu.Unlock()
	require.NoError(t, v.Activity(false))
	sent, flushes := fc.hear.counts()
	assert.Equal(t, 320, sent)
	assert.Equal(t, 1, flushes, "releasing settles what was said")
	fc.mu.Lock()
	fc.hold = make(chan struct{}) // the audio is slow in coming
	hold := fc.hold
	fc.mu.Unlock()
	fc.hear.events <- runtime.HearEvent{Kind: runtime.HearFinal, Text: "please"}
	assert.Equal(t, "speaking", evs.next(t, "state").State)

	fc.hear.events <- runtime.HearEvent{Kind: runtime.HearInterim, Text: "mm"}
	time.Sleep(50 * time.Millisecond)
	evs.mu.Lock()
	for _, e := range evs.evs {
		assert.NotEqual(t, "interrupted", e.Kind, "a noisy room: a sound isn't the person")
	}
	evs.mu.Unlock()
	require.NoError(t, v.Activity(true)) // pressing to talk stops it at once
	evs.next(t, "interrupted")
	close(hold)
}

// One thing said (VOX-17, VOX-41, PER-41): a sample in the editor's own
// tuning, nothing heard for it; and a turn's summary, in the
// personality's.
func TestCloudVoiceSampleAndSummary(t *testing.T) {
	w, fc, llm := openCloudVoice(t, nil, voiceText("Hi, I'm Blitz."), voiceText("Nothing yet."))
	evs := newVoiceEvents()
	tuning := config.VoiceTuning{Locale: "fr-CA", Speed: 1.2, Pitch: 3}
	v, err := w.StartVoice(context.Background(), VoiceOptions{SessionID: "s", SampleVoice: "Puck", SampleTuning: &tuning, OnEvent: evs.on})
	require.NoError(t, err)
	assert.Equal(t, "summary", evs.next(t, "end").Text)
	<-v.Done()
	spoke := fc.said()
	require.Len(t, spoke, 1)
	assert.Equal(t, "Puck", spoke[0].voice)
	assert.Equal(t, tuning, spoke[0].tuning)
	assert.Equal(t, []string{"Hi, I'm Blitz."}, spoke[0].texts)
	fc.mu.Lock()
	assert.Empty(t, fc.language, "a sample hears nothing")
	fc.mu.Unlock()
	assert.Contains(t, llm.Requests[0].Contents[0].Parts[0].Text, "Hi, I'm Blitz")
	assert.Contains(t, instructionOf(llm.Requests[0]), "the locale fr-CA")

	evs = newVoiceEvents()
	v, err = w.StartVoice(context.Background(), VoiceOptions{SessionID: "s", SummaryOnly: true, OnEvent: evs.on})
	require.NoError(t, err)
	assert.Equal(t, "summary", evs.next(t, "end").Text)
	<-v.Done()
	spoke = fc.said()
	require.Len(t, spoke, 2)
	assert.Equal(t, config.DefaultVoice, spoke[1].voice)
	assert.Equal(t, config.VoiceTuning{Locale: config.DefaultVoiceLanguage}, spoke[1].tuning, "no personality: the language the person speaks")
}

// A tool running mid-reply ends that stream of speech (Text-to-Speech
// gives up a stream sent nothing for a few seconds), and what's written
// after it is another's, its audio after the first's.
func TestCloudVoiceSpeaksAroundTools(t *testing.T) {
	before := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{Text: "Let me check"}, {FunctionCall: &genai.FunctionCall{Name: "status", Args: map[string]any{}}}}}
	w, fc, _ := openCloudVoice(t, nil, before, voiceText("Nothing is running."))
	evs := newVoiceEvents()
	v, err := w.StartVoice(context.Background(), VoiceOptions{SessionID: "s", OnEvent: evs.on})
	require.NoError(t, err)
	t.Cleanup(v.Close)
	fc.hear.says("Where are we?")
	assert.Equal(t, "Let me check", strings.TrimSpace(string(evs.next(t, "audio").Audio)))
	assert.Equal(t, "Nothing is running.", strings.TrimSpace(string(evs.next(t, "audio").Audio)))
	assert.Equal(t, "listening", evs.next(t, "state").State)
	spoke := fc.said()
	require.Len(t, spoke, 2, "a stream each side of the tool")
	assert.Equal(t, []string{"Let me check"}, spoke[0].texts)
	assert.Equal(t, []string{"Nothing is running."}, spoke[1].texts)
}

// What goes wrong is said in words the person can act on (VOX-74):
// without Google Cloud's sign-in there's no conversation, and
// Speech-to-Text failing ends one, naming the API that's off.
func TestCloudVoiceProblems(t *testing.T) {
	w, fc, _ := openCloudVoice(t, nil)
	evs := newVoiceEvents()
	v, err := w.StartVoice(context.Background(), VoiceOptions{SessionID: "s", OnEvent: evs.on})
	require.NoError(t, err)
	fc.hear.events <- runtime.HearEvent{Kind: runtime.HearError, Err: status.Error(codes.PermissionDenied, "Cloud Speech-to-Text API has not been used in project test before or it is disabled: speech.googleapis.com SERVICE_DISABLED")}
	assert.Equal(t, "Google Cloud's Speech-to-Text API isn't turned on in the project test: turn it on in the Google Cloud console, then try again.", evs.next(t, "error").Text)
	assert.Equal(t, "error", evs.next(t, "end").Text)
	<-v.Done()

	w.newCloudVoice = func(context.Context, *config.Config) (*runtime.CloudVoice, error) {
		return nil, fmt.Errorf("%w: no credentials", runtime.ErrVoiceNotSetUp)
	}
	_, err = w.StartVoice(context.Background(), VoiceOptions{SessionID: "s"})
	assert.ErrorIs(t, err, runtime.ErrVoiceNotSetUp)

	w.newCloudVoice = func(context.Context, *config.Config) (*runtime.CloudVoice, error) {
		return runtime.NewCloudVoiceWith(nil, nil), nil
	}
	w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) {
		return nil, errors.New("no such model")
	}
	_, err = w.StartVoice(context.Background(), VoiceOptions{SessionID: "s"})
	assert.ErrorIs(t, err, runtime.ErrVoiceNotSetUp, "its text model can't be built")
}

// brokenSpeech is a stream of speech that fails: at its first words, or
// when its audio is asked for.
type brokenSpeech struct{ say, recv error }

func (b brokenSpeech) Say(string) error { return b.say }
func (b brokenSpeech) Finish() error    { return b.say }
func (b brokenSpeech) Close()           {}
func (b brokenSpeech) Recv() ([]byte, error) {
	if b.recv != nil {
		return nil, b.recv
	}
	return nil, io.EOF
}

// A reply that can't be said ends the conversation, saying why (VOX-74):
// Text-to-Speech refusing to start, refusing the words, failing as its
// audio comes; and the text model failing. Nothing is heard for one
// thing said, and Speech-to-Text that can't start is no conversation.
func TestCloudVoiceReplyFails(t *testing.T) {
	off := status.Error(codes.PermissionDenied, "Cloud Text-to-Speech API has not been used in project test before or it is disabled: texttospeech.googleapis.com")
	for name, tc := range map[string]struct {
		speak func(context.Context, string, config.VoiceTuning) (runtime.SpeechStream, error)
		model error
		want  string
	}{
		"Text-to-Speech won't start": {speak: func(context.Context, string, config.VoiceTuning) (runtime.SpeechStream, error) { return nil, off }, want: "Text-to-Speech API isn't turned on in the project test"},
		"it refuses the words": {speak: func(context.Context, string, config.VoiceTuning) (runtime.SpeechStream, error) {
			return brokenSpeech{say: off}, nil
		}, want: "Text-to-Speech API isn't turned on"},
		"its audio fails": {speak: func(context.Context, string, config.VoiceTuning) (runtime.SpeechStream, error) {
			return brokenSpeech{recv: status.Error(codes.ResourceExhausted, "quota")}, nil
		}, want: "quota for speech is spent"},
		"the text model fails": {model: errors.New("the model is away"), want: "The voice conversation stopped"},
	} {
		t.Run(name, func(t *testing.T) {
			w, fc, _ := openCloudVoice(t, nil, voiceText("Hello there. How are you?"))
			if tc.speak != nil {
				w.newCloudVoice = func(context.Context, *config.Config) (*runtime.CloudVoice, error) {
					return runtime.NewCloudVoiceWith(tc.speak, func(context.Context, string) (runtime.Listener, error) { return fc.hear, nil }), nil
				}
			}
			if tc.model != nil {
				w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) { return awayLLM{tc.model}, nil }
			}
			evs := newVoiceEvents()
			v, err := w.StartVoice(context.Background(), VoiceOptions{SessionID: "s", OnEvent: evs.on})
			require.NoError(t, err)
			fc.hear.says("Hello")
			assert.Contains(t, evs.next(t, "error").Text, tc.want)
			assert.Equal(t, "error", evs.next(t, "end").Text)
			<-v.Done()
		})
	}

	w, _, _ := openCloudVoice(t, nil, voiceText("Hi."))
	evs := newVoiceEvents()
	v, err := w.StartVoice(context.Background(), VoiceOptions{SessionID: "s", SampleVoice: "Kore", OnEvent: evs.on})
	require.NoError(t, err)
	assert.NoError(t, v.Audio(make([]byte, 320)), "one thing said: nothing is heard")
	assert.NoError(t, v.Activity(true))
	assert.Equal(t, "summary", evs.next(t, "end").Text)
	<-v.Done()
	assert.Error(t, v.tell("too late"), "ended: nothing more is said")

	w.newCloudVoice = func(context.Context, *config.Config) (*runtime.CloudVoice, error) {
		return runtime.NewCloudVoiceWith(nil, func(context.Context, string) (runtime.Listener, error) { return nil, errors.New("no stream to be had") }), nil
	}
	_, err = w.StartVoice(context.Background(), VoiceOptions{SessionID: "s"})
	assert.ErrorContains(t, err, "no stream to be had")
}

// awayLLM is a text model that fails.
type awayLLM struct{ err error }

func (awayLLM) Name() string { return "failing" }

func (f awayLLM) GenerateContent(context.Context, *model.LLMRequest, bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) { yield(nil, f.err) }
}

// Left to auto (VOX-70a), the engine follows the personality: one that
// tunes its voice is spoken by the cloud engine, as is a sample tuned in
// the editor; one that doesn't, by a live model; and a tuned one too
// when Google Cloud isn't set up, rather than no voice at all.
func TestVoiceEngineAuto(t *testing.T) {
	tuned := func(c *config.Config) {
		require.NoError(t, SavePersonality(api.Personality{Name: "nagatha", Text: "Poised.", Voice: "Aoede", Locale: "en-GB", Pitch: -2}))
		c.UI.Personality = "nagatha"
	}
	auto := func(t *testing.T, w *Workspace) {
		t.Helper()
		_, err := config.SetValue(w.cfg.Dir, "", "voice.engine", "auto")
		require.NoError(t, err)
	}
	t.Run("a tuned personality: the cloud", func(t *testing.T) {
		w, fc, _ := openCloudVoice(t, tuned, voiceText("Hello."))
		auto(t, w)
		evs := newVoiceEvents()
		v, err := w.StartVoice(context.Background(), VoiceOptions{SessionID: "s", SummaryOnly: true, OnEvent: evs.on})
		require.NoError(t, err)
		assert.Equal(t, config.VoiceEngineCloud, v.Engine())
		assert.Equal(t, "summary", evs.next(t, "end").Text)
		<-v.Done()
		require.Len(t, fc.said(), 1)
		assert.Equal(t, config.VoiceTuning{Locale: "en-GB", Pitch: -2}, fc.said()[0].tuning)
	})
	t.Run("a sample tuned in the editor: the cloud", func(t *testing.T) {
		w, fc, _ := openCloudVoice(t, nil, voiceText("Hello."))
		auto(t, w)
		evs := newVoiceEvents()
		v, err := w.StartVoice(context.Background(), VoiceOptions{SessionID: "s", SampleVoice: "Puck", SampleTuning: &config.VoiceTuning{Speed: 1.2}, OnEvent: evs.on})
		require.NoError(t, err)
		assert.Equal(t, config.VoiceEngineCloud, v.Engine())
		assert.Equal(t, "summary", evs.next(t, "end").Text)
		<-v.Done()
		require.Len(t, fc.said(), 1)
	})
	t.Run("nothing tuned: a live model", func(t *testing.T) {
		w, live := openVoice(t, nil)
		w.newCloudVoice = func(context.Context, *config.Config) (*runtime.CloudVoice, error) {
			require.Fail(t, "the cloud engine was built for a voice that isn't tuned")
			return nil, nil
		}
		v, err := w.StartVoice(context.Background(), VoiceOptions{SessionID: "s"})
		require.NoError(t, err)
		t.Cleanup(v.Close)
		assert.Equal(t, config.VoiceEngineLive, v.Engine())
		live.Opened(wait)
	})
	t.Run("tuned, but Google Cloud isn't set up: a live model", func(t *testing.T) {
		w, live := openVoice(t, tuned)
		w.newCloudVoice = func(context.Context, *config.Config) (*runtime.CloudVoice, error) {
			return nil, runtime.CloudNotSetUp("no Google Cloud credentials")
		}
		v, err := w.StartVoice(context.Background(), VoiceOptions{SessionID: "s"})
		require.NoError(t, err)
		t.Cleanup(v.Close)
		assert.Equal(t, config.VoiceEngineLive, v.Engine())
		setup := live.Opened(wait)
		assert.Contains(t, fmt.Sprint(setup["generationConfig"]), "Aoede", "in the personality's voice still")

		// Set to the cloud, the same is no conversation: it says what's missing.
		_, err = config.SetValue(w.cfg.Dir, "", "voice.engine", "cloud")
		require.NoError(t, err)
		_, err = w.StartVoice(context.Background(), VoiceOptions{SessionID: "s2"})
		assert.ErrorIs(t, err, runtime.ErrVoiceNotSetUp)
	})
}
