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

package runtime

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cloud.google.com/go/auth"
	speech "cloud.google.com/go/speech/apiv2"
	"cloud.google.com/go/speech/apiv2/speechpb"
	texttospeech "cloud.google.com/go/texttospeech/apiv1"
	"cloud.google.com/go/texttospeech/apiv1/texttospeechpb"
	"github.com/retail-cortex/blitz/pkg/config"
	"google.golang.org/api/option"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The cloud voice engine's ears and mouth (spec_063_voice VOX-70 to
// VOX-74): Google Cloud's Speech-to-Text hears the person, a stream at a
// time, and Text-to-Speech's Chirp 3: HD voices say what the text model
// writes, in a personality's locale, at its speed and in its pitch. Both
// are reached with Google Cloud's own credentials (Application Default
// Credentials), in a project with the two APIs on: a Gemini API key isn't
// one of theirs.

// SpeechStream is one thing being said: text in, a sentence or more at a
// time, 24 kHz 16-bit mono PCM out as it's made.
type SpeechStream interface {
	// Say adds text to say; whole sentences sound best.
	Say(text string) error
	// Finish says no more text is coming: Recv ends once it's all said.
	Finish() error
	// Recv is the next audio, io.EOF after the last.
	Recv() ([]byte, error)
	// Close gives it up, said or not.
	Close()
}

// What a Listener hears.
const (
	// HearBegin and HearEnd are the person starting and stopping speaking.
	HearBegin = "begin"
	HearEnd   = "end"
	// HearInterim is the words so far of what's being said, which may
	// still change; HearFinal is them settled.
	HearInterim = "interim"
	HearFinal   = "final"
	// HearError is the listener giving up (Err).
	HearError = "error"
)

// HearEvent is one thing heard.
type HearEvent struct {
	Kind string
	Text string
	Err  error
}

// Listener hears the person: 16 kHz 16-bit mono PCM in, words out.
type Listener interface {
	// Send adds audio; it never waits.
	Send(pcm []byte)
	// Flush settles what's been heard without waiting for a pause (hold
	// to talk's release).
	Flush()
	// Events are what's heard, in order; closed when the listener ends.
	Events() <-chan HearEvent
	Close()
}

// CloudVoice is the cloud engine's Speech-to-Text and Text-to-Speech.
type CloudVoice struct {
	// Project is the Google Cloud project they're used in.
	Project string
	speak   func(ctx context.Context, voice string, t config.VoiceTuning) (SpeechStream, error)
	listen  func(ctx context.Context, language string) (Listener, error)
	close   func()
}

// NewCloudVoiceWith is a CloudVoice that speaks and listens with these
// (tests' own, in place of Google's).
func NewCloudVoiceWith(speak func(ctx context.Context, voice string, t config.VoiceTuning) (SpeechStream, error), listen func(ctx context.Context, language string) (Listener, error)) *CloudVoice {
	return &CloudVoice{Project: "test", speak: speak, listen: listen}
}

// Speak starts saying something in voice (one of config.GeminiVoices,
// which Chirp 3: HD's are named as), tuned by t.
func (c *CloudVoice) Speak(ctx context.Context, voice string, t config.VoiceTuning) (SpeechStream, error) {
	return c.speak(ctx, voice, t)
}

// Listen starts hearing someone who speaks language (BCP-47).
func (c *CloudVoice) Listen(ctx context.Context, language string) (Listener, error) {
	return c.listen(ctx, language)
}

// Close lets go of its connections.
func (c *CloudVoice) Close() {
	if c.close != nil {
		c.close()
	}
}

// ChirpVoice is the Text-to-Speech name of voice in locale:
// "en-GB-Chirp3-HD-Aoede".
func ChirpVoice(locale, voice string) string {
	return cmp.Or(locale, config.DefaultVoiceLanguage) + "-Chirp3-HD-" + cmp.Or(voice, config.DefaultVoice)
}

// cloudProject is the Google Cloud project the cloud engine works in:
// [llm.gemini] project_id, GOOGLE_CLOUD_PROJECT, else the credentials'
// own (their quota project, or a service account's).
func cloudProject(ctx context.Context, cfg *config.Config, quota, own func(context.Context) (string, error)) string {
	if p := cmp.Or(cfg.LLM.Gemini.ProjectID, os.Getenv("GOOGLE_CLOUD_PROJECT")); p != "" {
		return p
	}
	for _, f := range []func(context.Context) (string, error){quota, own} {
		if p, err := f(ctx); err == nil && p != "" {
			return p
		}
	}
	return ""
}

// cloudNotSetUp is the cloud engine missing something of its own: an
// ErrVoiceNotSetUp (VOICE_NOT_SET_UP) that says what, not that Gemini is
// missing.
type cloudNotSetUp struct{ why string }

func (e cloudNotSetUp) Error() string { return e.why }

// Is reports it as ErrVoiceNotSetUp.
func (e cloudNotSetUp) Is(target error) bool { return target == ErrVoiceNotSetUp }

// CloudNotSetUp is the cloud voice engine not being set up, for why
// (VOX-74): errors.Is finds ErrVoiceNotSetUp in it.
func CloudNotSetUp(why string) error { return cloudNotSetUp{why} }

// cloudAccess is the Google Cloud credentials and project the cloud
// engine works with, or ErrVoiceNotSetUp saying what's missing; with
// signIn, the credentials are tried too (a token asked for).
func cloudAccess(ctx context.Context, cfg *config.Config, signIn bool) (*auth.Credentials, string, error) {
	if err := ownProviderOff(cfg, "gemini"); err != nil {
		return nil, "", CloudNotSetUp(err.Error())
	}
	creds, err := googleCredentials()
	if err != nil {
		return nil, "", CloudNotSetUp(fmt.Sprintf("The Google Cloud voice engine needs Google Cloud credentials: sign in with `gcloud auth application-default login` on the machine running Blitz (%v)", err))
	}
	if signIn {
		if _, err := creds.Token(ctx); err != nil {
			return nil, "", CloudNotSetUp(cmp.Or(CloudVoiceProblem(err, ""), err.Error()))
		}
	}
	project := cloudProject(ctx, cfg, creds.QuotaProjectID, creds.ProjectID)
	if project == "" {
		return nil, "", CloudNotSetUp("The Google Cloud voice engine needs a Google Cloud project: set [llm.gemini] project_id or GOOGLE_CLOUD_PROJECT")
	}
	return creds, project, nil
}

// CloudVoiceProject is the Google Cloud project the cloud engine would
// work in, or why it can't be used (ErrVoiceNotSetUp, VOX-74); with
// signIn it tries the credentials, which asks Google.
func CloudVoiceProject(ctx context.Context, cfg *config.Config, signIn bool) (string, error) {
	_, project, err := cloudAccess(ctx, cfg, signIn)
	return project, err
}

// NewCloudVoice builds the cloud engine's Speech-to-Text and
// Text-to-Speech from cfg (VOX-70): ErrVoiceNotSetUp, saying what's
// missing, without Google Cloud credentials that still sign in, or
// without a project.
func NewCloudVoice(ctx context.Context, cfg *config.Config) (*CloudVoice, error) {
	// Its sign-in is tried now, not when the person first speaks.
	creds, project, err := cloudAccess(ctx, cfg, true)
	if err != nil {
		return nil, err
	}
	opts := []option.ClientOption{option.WithAuthCredentials(creds), option.WithQuotaProject(project)}
	tts, err := texttospeech.NewClient(ctx, opts...)
	if err != nil {
		return nil, CloudNotSetUp(err.Error())
	}
	location := cmp.Or(cfg.Voice.HearLocation, config.DefaultVoiceHearLocation)
	sttOpts := opts
	if location != "global" {
		sttOpts = append(sttOpts[:len(sttOpts):len(sttOpts)], option.WithEndpoint(location+"-speech.googleapis.com:443"))
	}
	stt, err := speech.NewClient(ctx, sttOpts...)
	if err != nil {
		tts.Close()
		return nil, CloudNotSetUp(err.Error())
	}
	hear := hearing{
		open:       func(ctx context.Context) (recognizeStream, error) { return stt.StreamingRecognize(ctx) },
		recognizer: fmt.Sprintf("projects/%s/locations/%s/recognizers/_", project, location),
		model:      cmp.Or(cfg.Voice.HearModel, config.DefaultVoiceHearModel),
	}
	return &CloudVoice{
		Project: project,
		speak: func(ctx context.Context, voice string, t config.VoiceTuning) (SpeechStream, error) {
			return newSpeech(ctx, func(ctx context.Context) (synthesizeStream, error) { return tts.StreamingSynthesize(ctx) }, voice, t)
		},
		listen: func(ctx context.Context, language string) (Listener, error) {
			h := hear
			h.language = language
			return newListener(ctx, h), nil
		},
		close: func() {
			_ = tts.Close()
			_ = stt.Close()
		},
	}, nil
}

// CloudVoiceProblem is err from Google Cloud's speech APIs as the person
// is told it (VOX-74): what to do about a sign-in that has run out, an
// API that's off, a model or voice that isn't there; "" for an error
// that says nothing of the kind.
func CloudVoiceProblem(err error, project string) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	in := ""
	if project != "" {
		in = " in the project " + project
	}
	switch code := status.Code(err); {
	case strings.Contains(s, "invalid_grant"), strings.Contains(s, "reauth"), code == codes.Unauthenticated:
		return "Google Cloud's sign-in has run out: sign in again with `gcloud auth application-default login` on the machine running Blitz."
	case strings.Contains(s, "SERVICE_DISABLED"), strings.Contains(s, "has not been used in project"), strings.Contains(s, "is disabled"):
		api := "Text-to-Speech and Speech-to-Text APIs"
		switch {
		case strings.Contains(s, "texttospeech"):
			api = "Text-to-Speech API"
		case strings.Contains(s, "speech.googleapis.com"):
			api = "Speech-to-Text API"
		}
		return fmt.Sprintf("Google Cloud's %s isn't turned on%s: turn it on in the Google Cloud console, then try again.", api, in)
	case code == codes.PermissionDenied:
		return fmt.Sprintf("Google Cloud refused the voice%s: the account signed in needs the Text-to-Speech and Speech-to-Text APIs there.", in)
	case code == codes.ResourceExhausted:
		return "Google Cloud's quota for speech is spent for now: try again later."
	case code == codes.NotFound, code == codes.InvalidArgument:
		return "Google Cloud doesn't take the voice's settings (its locale, or Speech-to-Text's model or location): " + status.Convert(err).Message()
	}
	return ""
}

// ---- Text-to-Speech.

// synthesizeStream is the part of Text-to-Speech's stream a speech uses.
type synthesizeStream interface {
	Send(*texttospeechpb.StreamingSynthesizeRequest) error
	Recv() (*texttospeechpb.StreamingSynthesizeResponse, error)
	CloseSend() error
}

// speechRate is the sample rate the voice is asked for and sent at.
const speechRate = 24000

// speech is a SpeechStream over Text-to-Speech's streaming synthesis.
type speechStream struct {
	s      synthesizeStream
	cancel context.CancelFunc
	shift  *pitchShifter // nil: the voice's own pitch
	ended  bool
}

// newSpeech opens a stream for voice tuned by t: its locale in the
// voice's name, its speed and pitch in the speaking rate and the shifter
// (VOX-72).
func newSpeech(ctx context.Context, open func(context.Context) (synthesizeStream, error), voice string, t config.VoiceTuning) (SpeechStream, error) {
	ctx, cancel := context.WithCancel(ctx)
	s, err := open(ctx)
	if err != nil {
		cancel()
		return nil, err
	}
	locale := cmp.Or(t.Locale, config.DefaultVoiceLanguage)
	rate := math.Max(config.MinSpeakingRate, math.Min(config.MaxSpeakingRate, t.SpeakingRate()))
	if err := s.Send(&texttospeechpb.StreamingSynthesizeRequest{StreamingRequest: &texttospeechpb.StreamingSynthesizeRequest_StreamingConfig{
		StreamingConfig: &texttospeechpb.StreamingSynthesizeConfig{
			Voice:                &texttospeechpb.VoiceSelectionParams{LanguageCode: locale, Name: ChirpVoice(locale, voice)},
			StreamingAudioConfig: &texttospeechpb.StreamingAudioConfig{AudioEncoding: texttospeechpb.AudioEncoding_PCM, SampleRateHertz: speechRate, SpeakingRate: rate},
		},
	}}); err != nil {
		cancel()
		return nil, err
	}
	return &speechStream{s: s, cancel: cancel, shift: newPitchShifter(t.PitchFactor())}, nil
}

func (s *speechStream) Say(text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return s.s.Send(&texttospeechpb.StreamingSynthesizeRequest{StreamingRequest: &texttospeechpb.StreamingSynthesizeRequest_Input{
		Input: &texttospeechpb.StreamingSynthesisInput{InputSource: &texttospeechpb.StreamingSynthesisInput_Text{Text: text}},
	}})
}

func (s *speechStream) Finish() error { return s.s.CloseSend() }

func (s *speechStream) Recv() ([]byte, error) {
	for {
		if s.ended {
			return nil, io.EOF
		}
		r, err := s.s.Recv()
		if errors.Is(err, io.EOF) {
			s.ended = true
			if s.shift != nil {
				if rest := s.shift.flush(); len(rest) > 0 {
					return rest, nil
				}
			}
			return nil, io.EOF
		}
		if err != nil {
			return nil, err
		}
		pcm := r.GetAudioContent()
		if s.shift != nil {
			pcm = s.shift.write(pcm)
		}
		if len(pcm) > 0 {
			return pcm, nil
		}
	}
}

func (s *speechStream) Close() { s.cancel() }

// ---- Speech-to-Text.

// recognizeStream is the part of Speech-to-Text's stream a listener uses.
type recognizeStream interface {
	Send(*speechpb.StreamingRecognizeRequest) error
	Recv() (*speechpb.StreamingRecognizeResponse, error)
	CloseSend() error
}

// How a listener keeps its streams (VOX-73). Speech-to-Text ends a
// stream after five minutes, and one that's sent nothing for a while, so
// a stream is open only while audio comes: one that has gone quiet (the
// microphone muted, hold to talk released) is ended, which settles what
// it heard, and the next audio opens another. A stream near its limit is
// ended between sentences.
var (
	hearQuiet   = 2 * time.Second
	hearRotate  = 4 * time.Minute
	hearLongest = 4*time.Minute + 45*time.Second
	hearSettle  = 5 * time.Second
	hearRetries = 3
)

// maxHearChunk is the most audio in one message (the API's limit is 15 KB).
const maxHearChunk = 12800

// hearing is what a listener's streams are opened with.
type hearing struct {
	open       func(context.Context) (recognizeStream, error)
	recognizer string
	model      string
	language   string
}

// listener is a Listener over Speech-to-Text's streaming recognition.
type listener struct {
	hearing
	ctx    context.Context
	cancel context.CancelFunc
	audio  chan []byte
	flush  chan struct{}
	events chan HearEvent
	// speaking is whether the stream says the person is mid-sentence.
	speaking atomic.Bool
	once     sync.Once
}

func newListener(ctx context.Context, h hearing) *listener {
	ctx, cancel := context.WithCancel(ctx)
	l := &listener{hearing: h, ctx: ctx, cancel: cancel, audio: make(chan []byte, 512), flush: make(chan struct{}, 1), events: make(chan HearEvent, 64)}
	go l.run()
	return l
}

func (l *listener) Send(pcm []byte) {
	for len(pcm) > 0 {
		n := min(len(pcm), maxHearChunk)
		select {
		case l.audio <- pcm[:n:n]:
		default: // a listener that can't keep up loses audio, not the conversation
			slog.Debug("voice: audio dropped, Speech-to-Text is behind", "logger", "voice")
		}
		pcm = pcm[n:]
	}
}

func (l *listener) Flush() {
	select {
	case l.flush <- struct{}{}:
	default:
	}
}

func (l *listener) Events() <-chan HearEvent { return l.events }

func (l *listener) Close() { l.once.Do(l.cancel) }

func (l *listener) emit(e HearEvent) {
	select {
	case l.events <- e:
	case <-l.ctx.Done():
	}
}

// run opens a stream when audio comes, and another when that one ends;
// a few failures in a row end the listener, with the last.
func (l *listener) run() {
	defer close(l.events)
	failures := 0
	for {
		var first []byte
		select {
		case <-l.ctx.Done():
			return
		case <-l.flush: // nothing open to settle
			continue
		case first = <-l.audio:
		}
		err := l.stream(first)
		switch {
		case l.ctx.Err() != nil:
			return
		case err == nil:
			failures = 0
		default:
			failures++
			slog.Warn("voice: Speech-to-Text's stream failed", "logger", "voice", "error", err, "failures", failures)
			if failures >= hearRetries || !retryableHear(err) {
				l.emit(HearEvent{Kind: HearError, Err: err})
				return
			}
			select {
			case <-l.ctx.Done():
				return
			case <-time.After(time.Duration(failures) * 300 * time.Millisecond):
			}
		}
	}
}

// retryableHear reports whether a stream's failure may be the stream's
// alone: not a sign-in, a permission or a setting Google refuses.
func retryableHear(err error) bool {
	switch status.Code(err) {
	case codes.Unauthenticated, codes.PermissionDenied, codes.InvalidArgument, codes.NotFound, codes.FailedPrecondition:
		return false
	}
	return !strings.Contains(err.Error(), "invalid_grant")
}

// stream hears one stream's worth: from first until the audio goes quiet,
// a flush, the stream's age, or its failure.
func (l *listener) stream(first []byte) error {
	ctx, cancel := context.WithCancel(l.ctx)
	defer cancel()
	s, err := l.open(ctx)
	if err != nil {
		return err
	}
	if err := s.Send(&speechpb.StreamingRecognizeRequest{
		Recognizer: l.recognizer,
		StreamingRequest: &speechpb.StreamingRecognizeRequest_StreamingConfig{StreamingConfig: &speechpb.StreamingRecognitionConfig{
			Config: &speechpb.RecognitionConfig{
				DecodingConfig: &speechpb.RecognitionConfig_ExplicitDecodingConfig{ExplicitDecodingConfig: &speechpb.ExplicitDecodingConfig{
					Encoding: speechpb.ExplicitDecodingConfig_LINEAR16, SampleRateHertz: 16000, AudioChannelCount: 1,
				}},
				Model: l.model, LanguageCodes: []string{l.language},
				Features: &speechpb.RecognitionFeatures{EnableAutomaticPunctuation: true},
			},
			StreamingFeatures: &speechpb.StreamingRecognitionFeatures{InterimResults: true, EnableVoiceActivityEvents: true},
		}},
	}); err != nil {
		return l.recvError(s, err)
	}
	l.speaking.Store(false)
	recv := make(chan error, 1)
	go func() { recv <- l.receive(s) }()
	send := func(pcm []byte) error {
		return s.Send(&speechpb.StreamingRecognizeRequest{StreamingRequest: &speechpb.StreamingRecognizeRequest_Audio{Audio: pcm}})
	}
	// settle ends the sending and waits for what the stream still has to
	// say: its last words.
	settle := func() error {
		_ = s.CloseSend()
		select {
		case err := <-recv:
			return err
		case <-time.After(hearSettle):
			return nil
		case <-l.ctx.Done():
			return nil
		}
	}
	if err := send(first); err != nil {
		return l.recvError(s, err)
	}
	opened := time.Now()
	quiet := time.NewTimer(hearQuiet)
	defer quiet.Stop()
	age := time.NewTicker(time.Second)
	defer age.Stop()
	for {
		select {
		case <-l.ctx.Done():
			return nil
		case err := <-recv:
			return err // the stream ended by itself: its limit, or a failure
		case pcm := <-l.audio:
			if err := send(pcm); err != nil {
				select {
				case rerr := <-recv: // why, from the stream
					return cmp.Or(rerr, err)
				case <-time.After(time.Second):
					return err
				}
			}
			quiet.Reset(hearQuiet)
		case <-l.flush:
			return settle()
		case <-quiet.C:
			return settle()
		case <-age.C:
			if old := time.Since(opened); old >= hearLongest || (old >= hearRotate && !l.speaking.Load()) {
				return settle()
			}
		}
	}
}

// recvError is the stream's own account of a send that failed, if it
// gives one soon (a send only says the stream has ended).
func (l *listener) recvError(s recognizeStream, err error) error {
	got := make(chan error, 1)
	go func() {
		_, rerr := s.Recv()
		got <- rerr
	}()
	select {
	case rerr := <-got:
		if rerr != nil && !errors.Is(rerr, io.EOF) {
			return rerr
		}
	case <-time.After(time.Second):
	}
	return err
}

// receive passes on what a stream hears, until it ends (nil at its end).
func (l *listener) receive(s recognizeStream) error {
	for {
		r, err := s.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			if l.ctx.Err() != nil || status.Code(err) == codes.Canceled {
				return nil
			}
			return err
		}
		switch r.GetSpeechEventType() {
		case speechpb.StreamingRecognizeResponse_SPEECH_ACTIVITY_BEGIN:
			l.speaking.Store(true)
			l.emit(HearEvent{Kind: HearBegin})
		case speechpb.StreamingRecognizeResponse_SPEECH_ACTIVITY_END:
			l.speaking.Store(false)
			l.emit(HearEvent{Kind: HearEnd})
		}
		for _, res := range r.GetResults() {
			if len(res.GetAlternatives()) == 0 {
				continue
			}
			text := strings.TrimSpace(res.GetAlternatives()[0].GetTranscript())
			if text == "" {
				continue
			}
			if res.GetIsFinal() {
				l.speaking.Store(false)
				l.emit(HearEvent{Kind: HearFinal, Text: text})
			} else {
				l.emit(HearEvent{Kind: HearInterim, Text: text})
			}
		}
	}
}
