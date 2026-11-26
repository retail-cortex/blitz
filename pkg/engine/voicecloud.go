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
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/genai"
)

// The cloud voice engine (spec_063_voice VOX-70 to VOX-75), [voice]
// engine = "cloud": what the person says is heard by Google Cloud's
// Speech-to-Text, read by a text model (the voice agent, with the tools
// and instructions the live one has), and what it writes is said by
// Text-to-Speech's Chirp 3: HD voices, a sentence at a time as it's
// written. A Gemini Live model speaks only in its own voice; these
// voices speak in the personality's locale, at its speed and in its
// pitch (spec_064_personality PER-41), the same everywhere Blitz speaks:
// a conversation, a summary, a sample.
//
// The engine takes the turns itself, which a live model does for its
// own: the person's words settle (a pause, or hold to talk's release),
// the agent answers, and words from the person while it speaks stop it
// (VOX-75).

// cloudWritten is added to the voice agent's instructions with the cloud
// engine: what it writes is read aloud as written.
const cloudWritten = "\n- Your words are turned into speech exactly as you write them: plain sentences only, with no symbols, emoji or Markdown, and numbers, units and abbreviations written as you'd say them."

// written is the instructions' note for an engine that speaks text.
func (v *Voice) written() string {
	if v.engine == config.VoiceEngineCloud {
		return cloudWritten
	}
	return ""
}

// tuning is how the voice is tuned (PER-41): a sample's own, else the
// personality's.
func (v *Voice) tuning() config.VoiceTuning {
	if t := v.opts.SampleTuning; t != nil {
		return *t
	}
	return personalityTuning(v.w.Personality())
}

// speaksIn is the locale the cloud engine's voice speaks: the tuning's,
// else the language the person speaks.
func (v *Voice) speaksIn() string {
	return cmp.Or(v.tuning().Locale, v.cfg.HearIn(""))
}

// cloudVoice builds what the cloud engine needs (VOX-70): its text
// model ([voice] text_model, else the workspace's small model) and
// Google Cloud's Speech-to-Text and Text-to-Speech.
func (w *Workspace) cloudVoice(ctx context.Context, cfg *config.Config) (*runtime.VoiceModel, *runtime.CloudVoice, error) {
	build := w.newCloudVoice
	if build == nil {
		build = runtime.NewCloudVoice
	}
	cloud, err := build(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	llm, err := w.newModel(ctx, cfg, cmp.Or(strings.TrimSpace(cfg.Voice.TextModel), cfg.Suggestions.Model, cfg.Permissions.Auto.Model))
	if err != nil {
		cloud.Close()
		return nil, nil, runtime.CloudNotSetUp(fmt.Sprintf("The Google Cloud voice engine's text model ([voice] text_model) can't be used: %v", err))
	}
	return &runtime.VoiceModel{LLM: llm, ID: llm.Name(), Project: cloud.Project}, cloud, nil
}

// How the cloud engine takes turns (VOX-75).
var (
	// cloudSettle is how long after the person's last settled words the
	// agent answers, when [voice] pause_ms doesn't say: Speech-to-Text
	// settles words at a pause already, so this only joins two sentences
	// said one after the other.
	cloudSettle = 350 * time.Millisecond
	// cloudLongSentence is how much text is said without waiting for its
	// sentence to end.
	cloudLongSentence = 280
)

// cloudLink is a conversation over the cloud engine.
type cloudLink struct {
	v      *Voice
	ctx    context.Context
	cloud  *runtime.CloudVoice
	runner *runner.Runner
	hear   runtime.Listener // nil: nothing to hear (a summary, a sample)
	voice  string
	tuning config.VoiceTuning
	hold   bool // hold to talk
	noisy  bool // only settled words interrupt
	settle time.Duration
	inputs chan cloudInput

	// out orders what the window is sent of a reply against its being
	// interrupted: no audio follows "interrupted".
	out sync.Mutex

	mu      sync.Mutex
	stop    context.CancelFunc // the reply being written (nil: none)
	until   time.Time          // when the audio sent will have played
	pending []string           // the person's settled words, not yet answered
	timer   *time.Timer
	holding bool
}

// cloudInput is something for the agent to answer: the person's words,
// or one of Blitz's notes.
type cloudInput struct{ text string }

// newCloudLink starts the cloud engine for v: it listens (unless v only
// has one thing to say) and answers what it's given, one at a time.
func newCloudLink(ctx context.Context, v *Voice, cloud *runtime.CloudVoice, r *runner.Runner) (*cloudLink, error) {
	c := &cloudLink{
		v: v, ctx: ctx, cloud: cloud, runner: r, voice: v.voiceName(), tuning: v.tuning(),
		hold: v.cfg.ListenMode() == config.VoiceListenHold, noisy: v.cfg.NoiseMode() == config.VoiceNoiseNoisy,
		settle: cloudSettle, inputs: make(chan cloudInput, 64),
	}
	// The voice speaks its own locale's language.
	c.tuning.Locale = v.speaksIn()
	if ms := v.cfg.Pause(); ms > 0 {
		c.settle = time.Duration(ms) * time.Millisecond
	}
	if !v.opts.SummaryOnly {
		hear, err := cloud.Listen(ctx, v.cfg.HearIn(v.tuning().Locale))
		if err != nil {
			return nil, err
		}
		c.hear = hear
		go c.listen()
	}
	go c.answer()
	return c, nil
}

func (c *cloudLink) audio(pcm []byte) error {
	if c.hear == nil {
		return nil
	}
	c.mu.Lock()
	deaf := c.hold && !c.holding
	c.mu.Unlock()
	if !deaf {
		c.hear.Send(pcm)
	}
	return nil
}

// activity is hold to talk (VOX-07): pressing stops what's being said,
// as speaking over it does; releasing settles what was said.
func (c *cloudLink) activity(start bool) error {
	if c.hear == nil {
		return nil
	}
	c.mu.Lock()
	c.holding = start
	c.mu.Unlock()
	if start {
		c.interrupt()
	} else {
		c.hear.Flush()
	}
	return nil
}

func (c *cloudLink) tell(text string) error {
	if err := c.ctx.Err(); err != nil {
		return err // ended: nothing more is said
	}
	select {
	case c.inputs <- cloudInput{text: text}:
		return nil
	case <-c.ctx.Done():
		return c.ctx.Err()
	default:
		return errors.New("the voice has too much waiting to be said")
	}
}

func (c *cloudLink) close() {
	c.mu.Lock()
	if c.timer != nil {
		c.timer.Stop()
	}
	c.mu.Unlock()
	if c.hear != nil {
		c.hear.Close()
	}
	c.cloud.Close()
}

// speaking reports whether the voice is being heard: a reply being
// written, or its audio still playing.
func (c *cloudLink) speaking() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stop != nil || time.Now().Before(c.until)
}

// interrupt stops what's being said (VOX-75): the reply being written
// is given up, and the window drops the audio it hasn't played.
func (c *cloudLink) interrupt() {
	c.out.Lock()
	defer c.out.Unlock()
	c.mu.Lock()
	stop, playing := c.stop, time.Now().Before(c.until)
	c.stop, c.until = nil, time.Time{}
	c.mu.Unlock()
	if stop == nil && !playing {
		return
	}
	if stop != nil {
		stop()
	}
	c.v.opts.OnEvent(VoiceEvent{Kind: "interrupted"})
}

// listen takes what Speech-to-Text hears: settled words are the
// person's line, answered once they've stopped; words while the voice
// speaks stop it.
func (c *cloudLink) listen() {
	for {
		var e runtime.HearEvent
		select {
		case <-c.ctx.Done():
			return
		case got, ok := <-c.hear.Events():
			if !ok {
				return
			}
			e = got
		}
		switch e.Kind {
		case runtime.HearBegin:
			c.v.touch()
			c.wait()
		case runtime.HearInterim:
			c.v.touch()
			c.wait()
			if !c.noisy {
				c.interrupt()
			}
		case runtime.HearFinal:
			c.interrupt()
			piece := e.Text
			c.mu.Lock()
			if len(c.pending) > 0 {
				piece = " " + piece
			}
			c.pending = append(c.pending, e.Text)
			held := c.hold && c.holding
			c.mu.Unlock()
			c.v.heardPiece(piece, false)
			if !held {
				c.soon()
			}
		case runtime.HearError:
			if c.ctx.Err() == nil {
				c.v.fail(e.Err)
			}
			return
		}
	}
}

// wait puts off answering: the person is still speaking.
func (c *cloudLink) wait() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.timer != nil && len(c.pending) > 0 {
		c.timer.Reset(c.settle)
	}
}

// soon answers the person's settled words once they've said no more for
// a moment.
func (c *cloudLink) soon() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.timer != nil {
		c.timer.Reset(c.settle)
		return
	}
	c.timer = time.AfterFunc(c.settle, func() {
		c.mu.Lock()
		text := strings.Join(c.pending, " ")
		c.pending = nil
		c.mu.Unlock()
		if strings.TrimSpace(text) == "" {
			return
		}
		select {
		case c.inputs <- cloudInput{text: text}:
		case <-c.ctx.Done():
		}
	})
}

// answer has the agent answer each thing it's given, in order.
func (c *cloudLink) answer() {
	for {
		select {
		case <-c.ctx.Done():
			return
		case in := <-c.inputs:
			c.reply(in)
		}
	}
}

// reply is one answer: the agent writes, and what it writes is said as
// its sentences end. Interrupted, what was said so far is kept.
func (c *cloudLink) reply(in cloudInput) {
	ctx, cancel := context.WithCancel(c.ctx)
	defer cancel()
	c.mu.Lock()
	c.stop = cancel
	c.mu.Unlock()
	said := &utterance{c: c, ctx: ctx}
	err := c.write(ctx, in.text, said)
	if serr := said.finish(); err == nil {
		err = serr
	}
	c.mu.Lock()
	c.stop = nil
	c.mu.Unlock()
	switch {
	case c.ctx.Err() != nil:
		return // the conversation ended
	case ctx.Err() != nil: // spoken over
	case err != nil:
		c.v.fail(err)
		return
	}
	c.v.turnDone()
}

// write runs the agent on text, passing what it writes to said: a
// reply's pieces as they stream, or the whole of one that didn't.
func (c *cloudLink) write(ctx context.Context, text string, said *utterance) error {
	streamed := false
	events := c.runner.Run(ctx, "person", "voice-"+c.v.opts.SessionID, genai.NewContentFromText(text, genai.RoleUser), agent.RunConfig{StreamingMode: agent.StreamingModeSSE})
	for ev, err := range events {
		if err != nil {
			return err
		}
		if ev == nil || ev.Author == "user" {
			continue
		}
		r := ev.LLMResponse
		if r.UsageMetadata != nil && !ev.Partial {
			slog.Debug("voice usage", "logger", "voice", "model", c.v.model.ID, "input_tokens", r.UsageMetadata.PromptTokenCount, "output_tokens", r.UsageMetadata.CandidatesTokenCount)
		}
		if r.Content != nil {
			for _, p := range r.Content.Parts {
				switch {
				case p == nil:
				case p.FunctionCall != nil:
					// A tool runs: what's written so far is said meanwhile.
					said.pause()
				case p.Text != "" && !p.Thought && (ev.Partial || !streamed):
					said.write(p.Text)
				}
			}
		}
		// A streamed reply's pieces are repeated, whole, at its end.
		streamed = ev.Partial
	}
	return nil
}

// utterance is what's said of one reply: text in as it's written, cut
// into sentences for Text-to-Speech, its audio out in order. A stream
// of speech is ended when a tool runs (Text-to-Speech gives up a stream
// that's sent nothing for a few seconds), and the text after it is
// another stream's.
type utterance struct {
	c   *cloudLink
	ctx context.Context
	buf strings.Builder // written, its sentence not yet ended

	stream runtime.SpeechStream
	last   chan struct{} // closed when the latest stream's audio is all sent
	mu     sync.Mutex
	err    error
}

var (
	cloudSentence = regexp.MustCompile(`[.!?…。！？]["')\]]*\s+|\n+`)
	// unspoken is what a text model writes that shouldn't be read out.
	unspoken = regexp.MustCompile("[*`#]+|\\[Blitz\\]")
)

// write is the next of what the agent writes.
func (u *utterance) write(text string) {
	u.c.v.saidPiece(text)
	u.buf.WriteString(text)
	rest := u.buf.String()
	cut := 0
	for _, m := range cloudSentence.FindAllStringIndex(rest, -1) {
		cut = m[1]
	}
	if cut == 0 && len(rest) > cloudLongSentence {
		cut = strings.LastIndexByte(rest, ' ') + 1
	}
	if cut > 0 {
		u.say(rest[:cut])
		u.buf.Reset()
		u.buf.WriteString(rest[cut:])
	}
}

// say sends text to be spoken, opening a stream for it when none is.
func (u *utterance) say(text string) {
	text = strings.TrimSpace(unspoken.ReplaceAllString(text, ""))
	if text == "" || u.ctx.Err() != nil || u.failed() != nil {
		return
	}
	if u.stream == nil {
		s, err := u.c.cloud.Speak(u.ctx, u.c.voice, u.c.tuning)
		if err != nil {
			u.fail(err)
			return
		}
		before, done := u.last, make(chan struct{})
		u.stream, u.last = s, done
		go u.play(s, before, done)
	}
	if err := u.stream.Say(text); err != nil {
		u.fail(err)
	}
}

// play sends a stream's audio to the window, after the stream before
// it has sent all of its own.
func (u *utterance) play(s runtime.SpeechStream, before, done chan struct{}) {
	defer close(done)
	defer s.Close()
	if before != nil {
		<-before
	}
	for {
		pcm, err := s.Recv()
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			if u.ctx.Err() == nil {
				u.fail(err)
			}
			return
		}
		u.c.sendAudio(u.ctx, pcm)
	}
}

// sendAudio sends the window a reply's audio, unless the reply was
// interrupted, and notes when it will have played.
func (c *cloudLink) sendAudio(ctx context.Context, pcm []byte) {
	c.out.Lock()
	defer c.out.Unlock()
	if ctx.Err() != nil || len(pcm) == 0 {
		return
	}
	c.v.opts.OnEvent(VoiceEvent{Kind: "audio", Audio: pcm})
	long := time.Duration(len(pcm)/2) * time.Second / 24000
	c.mu.Lock()
	if now := time.Now(); c.until.Before(now) {
		c.until = now
	}
	c.until = c.until.Add(long)
	c.mu.Unlock()
}

// pause says what's written so far and ends its stream, without waiting
// for the audio.
func (u *utterance) pause() {
	if rest := u.buf.String(); strings.TrimSpace(rest) != "" {
		u.say(rest)
	}
	u.buf.Reset()
	if u.stream != nil {
		if err := u.stream.Finish(); err != nil && u.ctx.Err() == nil {
			u.fail(err)
		}
		u.stream = nil
	}
}

// finish says the rest and waits until all of the reply's audio is sent
// (or the reply interrupted); it's why the reply couldn't be said, if
// it couldn't.
func (u *utterance) finish() error {
	u.pause()
	if u.last != nil {
		select {
		case <-u.last:
		case <-u.ctx.Done():
		}
	}
	return u.failed()
}

func (u *utterance) fail(err error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.err == nil {
		u.err = err
	}
}

func (u *utterance) failed() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.err
}
