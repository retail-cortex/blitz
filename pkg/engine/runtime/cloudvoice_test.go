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
	"context"
	"errors"
	"io"
	"math"
	"sync"
	"testing"
	"time"

	"cloud.google.com/go/auth"
	"cloud.google.com/go/speech/apiv2/speechpb"
	"cloud.google.com/go/texttospeech/apiv1/texttospeechpb"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// tone is seconds of a sine at hz, as 24 kHz 16-bit PCM.
func tone(hz, seconds float64) []byte {
	n := int(seconds * speechRate)
	out := make([]byte, 0, n*2)
	for i := range n {
		s := int16(12000 * math.Sin(2*math.Pi*hz*float64(i)/speechRate))
		out = append(out, byte(s), byte(s>>8))
	}
	return out
}

// pitchOf is the frequency of a tone, by its upward zero crossings.
func pitchOf(pcm []byte) float64 {
	crossings, prev := 0, int16(0)
	for i := 0; i+1 < len(pcm); i += 2 {
		s := int16(uint16(pcm[i]) | uint16(pcm[i+1])<<8)
		if prev < 0 && s >= 0 {
			crossings++
		}
		prev = s
	}
	return float64(crossings) / (float64(len(pcm)/2) / speechRate)
}

// loudest is the largest sample.
func loudest(pcm []byte) int {
	m := 0
	for i := 0; i+1 < len(pcm); i += 2 {
		s := int(int16(uint16(pcm[i]) | uint16(pcm[i+1])<<8))
		m = max(m, max(s, -s))
	}
	return m
}

// A pitch is made by reading the audio faster or slower (VOX-72): the
// tone comes out that much higher and shorter, as loud as it went in,
// and the same whether the audio came whole or in pieces.
func TestPitchShifter(t *testing.T) {
	for _, tc := range []struct {
		name      string
		semitones float64
	}{
		{"two semitones up", 2},
		{"a fourth down", -5},
		{"half an octave up", 6},
		{"half an octave down", -6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			factor := config.VoiceTuning{Pitch: tc.semitones}.PitchFactor()
			in := tone(440, 1)
			whole := newPitchShifter(factor)
			out := append(whole.write(in), whole.flush()...)
			assert.InDelta(t, float64(len(in))/factor, float64(len(out)), 8, "shorter by as much as it's higher")
			assert.InDelta(t, 440*factor, pitchOf(out), 3)
			assert.InDelta(t, 12000, loudest(out[2000:len(out)-2000]), 400, "as loud")

			pieces := newPitchShifter(factor)
			var got []byte
			for i := 0; i < len(in); i += 333 { // odd pieces: a sample cut in two
				got = append(got, pieces.write(in[i:min(i+333, len(in))])...)
			}
			got = append(got, pieces.flush()...)
			assert.Equal(t, out, got, "the same in pieces")
		})
	}
	assert.Nil(t, newPitchShifter(1), "its own pitch: nothing to do")
	assert.Nil(t, newPitchShifter(0))
	assert.Nil(t, newPitchShifter(math.NaN()))
	assert.Empty(t, newPitchShifter(1.2).flush(), "nothing in, nothing out")
}

// fakeSynth is Text-to-Speech's stream: what it was sent, and the audio
// it answers with once the sending ends.
type fakeSynth struct {
	mu     sync.Mutex
	sent   []*texttospeechpb.StreamingSynthesizeRequest
	audio  [][]byte
	closed chan struct{}
	fail   error
}

func (f *fakeSynth) Send(r *texttospeechpb.StreamingSynthesizeRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, r)
	return nil
}

func (f *fakeSynth) CloseSend() error { close(f.closed); return nil }

func (f *fakeSynth) Recv() (*texttospeechpb.StreamingSynthesizeResponse, error) {
	<-f.closed
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	if len(f.audio) == 0 {
		return nil, io.EOF
	}
	a := f.audio[0]
	f.audio = f.audio[1:]
	return &texttospeechpb.StreamingSynthesizeResponse{AudioContent: a}, nil
}

// What's said is asked of Text-to-Speech in the personality's locale and
// voice, at the rate that comes out at its speed once its pitch is made
// (VOX-71, VOX-72): Chirp 3: HD's name, PCM at 24 kHz, the rate within
// what the API takes.
func TestSpeechStream(t *testing.T) {
	for _, tc := range []struct {
		name            string
		voice           string
		tuning          config.VoiceTuning
		wantVoice, lang string
		wantRate        float64
		shifted         bool
	}{
		{"its own", "", config.VoiceTuning{}, "en-US-Chirp3-HD-Kore", "en-US", 1, false},
		{"a locale and a speed", "Aoede", config.VoiceTuning{Locale: "en-GB", Speed: 0.9}, "en-GB-Chirp3-HD-Aoede", "en-GB", 0.9, false},
		{"lower: asked for faster, played slower", "Leda", config.VoiceTuning{Locale: "fr-CA", Speed: 1, Pitch: -2}, "fr-CA-Chirp3-HD-Leda", "fr-CA", math.Pow(2, 2.0/12), true},
		{"beyond the API's rate: held to it", "Puck", config.VoiceTuning{Speed: 2, Pitch: -6}, "en-US-Chirp3-HD-Puck", "en-US", 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeSynth{closed: make(chan struct{}), audio: [][]byte{tone(440, 0.2), tone(440, 0.2)}}
			s, err := newSpeech(context.Background(), func(context.Context) (synthesizeStream, error) { return f, nil }, tc.voice, tc.tuning)
			require.NoError(t, err)
			require.NoError(t, s.Say("Hello there."))
			require.NoError(t, s.Say("  "), "nothing to say isn't sent")
			require.NoError(t, s.Finish())
			var out []byte
			for {
				pcm, err := s.Recv()
				if errors.Is(err, io.EOF) {
					break
				}
				require.NoError(t, err)
				out = append(out, pcm...)
			}
			_, err = s.Recv()
			assert.ErrorIs(t, err, io.EOF, "ended stays ended")
			s.Close()

			require.Len(t, f.sent, 2)
			cfg := f.sent[0].GetStreamingConfig()
			assert.Equal(t, tc.wantVoice, cfg.Voice.Name)
			assert.Equal(t, tc.lang, cfg.Voice.LanguageCode)
			assert.Equal(t, texttospeechpb.AudioEncoding_PCM, cfg.StreamingAudioConfig.AudioEncoding)
			assert.EqualValues(t, speechRate, cfg.StreamingAudioConfig.SampleRateHertz)
			assert.InDelta(t, tc.wantRate, cfg.StreamingAudioConfig.SpeakingRate, 1e-9)
			assert.Equal(t, "Hello there.", f.sent[1].GetInput().GetText())
			if tc.shifted {
				assert.InDelta(t, 440*tc.tuning.PitchFactor(), pitchOf(out), 4, "in its pitch")
			} else {
				assert.Len(t, out, 2*len(tone(440, 0.2)), "as Text-to-Speech made it")
			}
		})
	}

	_, err := newSpeech(context.Background(), func(context.Context) (synthesizeStream, error) { return nil, errors.New("no stream") }, "Kore", config.VoiceTuning{})
	assert.ErrorContains(t, err, "no stream")
	f := &fakeSynth{closed: make(chan struct{}), fail: status.Error(codes.Aborted, "gave up")}
	s, err := newSpeech(context.Background(), func(context.Context) (synthesizeStream, error) { return f, nil }, "Kore", config.VoiceTuning{})
	require.NoError(t, err)
	require.NoError(t, s.Finish())
	_, err = s.Recv()
	assert.Equal(t, codes.Aborted, status.Code(err), "a stream's failure is the speech's")
}

// fakeRecognizer is Speech-to-Text: each stream opened, what it was
// sent, and what a test has it answer.
type fakeRecognizer struct {
	mu      sync.Mutex
	streams []*fakeRecognize
	opened  chan *fakeRecognize
	fail    error
}

type fakeRecognize struct {
	mu     sync.Mutex
	sent   []*speechpb.StreamingRecognizeRequest
	out    chan *speechpb.StreamingRecognizeResponse
	err    chan error
	closed chan struct{}
	once   sync.Once
	ctx    context.Context
}

func (f *fakeRecognizer) open(ctx context.Context) (recognizeStream, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	s := &fakeRecognize{out: make(chan *speechpb.StreamingRecognizeResponse, 16), err: make(chan error, 1), closed: make(chan struct{}), ctx: ctx}
	f.streams = append(f.streams, s)
	f.opened <- s
	return s, nil
}

func (s *fakeRecognize) Send(r *speechpb.StreamingRecognizeRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, r)
	return nil
}

func (s *fakeRecognize) CloseSend() error { s.once.Do(func() { close(s.closed) }); return nil }

func (s *fakeRecognize) Recv() (*speechpb.StreamingRecognizeResponse, error) {
	select {
	case r := <-s.out:
		return r, nil
	case err := <-s.err:
		return nil, err
	case <-s.closed:
		select {
		case r := <-s.out: // its last words
			return r, nil
		default:
			return nil, io.EOF
		}
	case <-s.ctx.Done():
		return nil, status.Error(codes.Canceled, "cancelled")
	}
}

func (s *fakeRecognize) audio() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, r := range s.sent {
		n += len(r.GetAudio())
	}
	return n
}

func heard(text string, final bool) *speechpb.StreamingRecognizeResponse {
	return &speechpb.StreamingRecognizeResponse{Results: []*speechpb.StreamingRecognitionResult{{IsFinal: final, Alternatives: []*speechpb.SpeechRecognitionAlternative{{Transcript: text}}}}}
}

// quickHearing shortens the listener's waits for a test.
func quickHearing(t *testing.T) {
	t.Helper()
	q, r, l, s := hearQuiet, hearRotate, hearLongest, hearSettle
	t.Cleanup(func() { hearQuiet, hearRotate, hearLongest, hearSettle = q, r, l, s })
	hearQuiet, hearSettle = 150*time.Millisecond, 200*time.Millisecond
}

func testListener(t *testing.T) (*listener, *fakeRecognizer) {
	t.Helper()
	quickHearing(t)
	f := &fakeRecognizer{opened: make(chan *fakeRecognize, 8)}
	l := newListener(context.Background(), hearing{open: f.open, recognizer: "projects/p/locations/global/recognizers/_", model: "long", language: "en-GB"})
	t.Cleanup(func() {
		l.Close()
		for range l.Events() {
		}
	})
	return l, f
}

func nextStream(t *testing.T, f *fakeRecognizer) *fakeRecognize {
	t.Helper()
	select {
	case s := <-f.opened:
		return s
	case <-time.After(5 * time.Second):
		require.Fail(t, "no stream opened")
		return nil
	}
}

func nextHeard(t *testing.T, l *listener) HearEvent {
	t.Helper()
	select {
	case e, ok := <-l.Events():
		require.True(t, ok, "the listener ended")
		return e
	case <-time.After(5 * time.Second):
		require.Fail(t, "nothing heard")
		return HearEvent{}
	}
}

// The listener's streams (VOX-73): none until there's audio; each opened
// with the recognizer, the model and the person's language, for 16 kHz
// PCM, interim words and voice activity; audio in messages the API
// takes; what it hears passed on as it comes. A stream gone quiet is
// ended, which settles its last words, and the next audio opens another;
// so does a flush, at once.
func TestListenerStreams(t *testing.T) {
	l, f := testListener(t)
	select {
	case <-f.opened:
		require.Fail(t, "a stream opened with nothing to hear")
	case <-time.After(50 * time.Millisecond):
	}
	l.Flush() // nothing open: nothing to settle

	l.Send(make([]byte, 2*maxHearChunk+100))
	s := nextStream(t, f)
	require.Eventually(t, func() bool { return s.audio() == 2*maxHearChunk+100 }, 5*time.Second, 5*time.Millisecond)
	s.mu.Lock()
	first := s.sent[0]
	for _, r := range s.sent[1:] {
		assert.LessOrEqual(t, len(r.GetAudio()), maxHearChunk)
	}
	s.mu.Unlock()
	assert.Equal(t, "projects/p/locations/global/recognizers/_", first.Recognizer)
	cfg := first.GetStreamingConfig()
	assert.Equal(t, "long", cfg.Config.Model)
	assert.Equal(t, []string{"en-GB"}, cfg.Config.LanguageCodes)
	dec := cfg.Config.GetExplicitDecodingConfig()
	assert.Equal(t, speechpb.ExplicitDecodingConfig_LINEAR16, dec.Encoding)
	assert.EqualValues(t, 16000, dec.SampleRateHertz)
	assert.True(t, cfg.StreamingFeatures.InterimResults)
	assert.True(t, cfg.StreamingFeatures.EnableVoiceActivityEvents)

	s.out <- &speechpb.StreamingRecognizeResponse{SpeechEventType: speechpb.StreamingRecognizeResponse_SPEECH_ACTIVITY_BEGIN}
	s.out <- heard("run the", false)
	s.out <- heard("  ", false) // nothing in it: not passed on
	s.out <- heard(" Run the tests. ", true)
	s.out <- &speechpb.StreamingRecognizeResponse{SpeechEventType: speechpb.StreamingRecognizeResponse_SPEECH_ACTIVITY_END}
	assert.Equal(t, HearEvent{Kind: HearBegin}, nextHeard(t, l))
	assert.Equal(t, HearEvent{Kind: HearInterim, Text: "run the"}, nextHeard(t, l))
	assert.Equal(t, HearEvent{Kind: HearFinal, Text: "Run the tests."}, nextHeard(t, l))
	assert.Equal(t, HearEvent{Kind: HearEnd}, nextHeard(t, l))

	// Quiet: the stream is ended, and what it says last still comes.
	select {
	case <-s.closed:
	case <-time.After(5 * time.Second):
		require.Fail(t, "a quiet stream wasn't ended")
	}
	l.Send(make([]byte, 320))
	s2 := nextStream(t, f)
	require.Eventually(t, func() bool { return s2.audio() == 320 }, 5*time.Second, 5*time.Millisecond)
	s2.out <- heard("and lint", true)
	l.Flush()
	select {
	case <-s2.closed:
	case <-time.After(5 * time.Second):
		require.Fail(t, "a flush didn't end the stream")
	}
	assert.Equal(t, HearEvent{Kind: HearFinal, Text: "and lint"}, nextHeard(t, l), "its last words, settled by the flush")
}

// A stream near Speech-to-Text's five minutes is ended between
// sentences, and at its limit whatever is being said; one that fails is
// tried again, and after a few failures, or one that's no stream's
// fault (a sign-in, an API that's off), the listener says so and ends.
func TestListenerLimitsAndFailures(t *testing.T) {
	t.Run("an old stream is ended between sentences", func(t *testing.T) {
		l, f := testListener(t)
		hearQuiet, hearRotate, hearLongest = time.Hour, time.Millisecond, time.Hour
		l.Send(make([]byte, 320))
		s := nextStream(t, f)
		s.out <- &speechpb.StreamingRecognizeResponse{SpeechEventType: speechpb.StreamingRecognizeResponse_SPEECH_ACTIVITY_BEGIN}
		nextHeard(t, l)
		select {
		case <-s.closed:
			require.Fail(t, "ended mid-sentence")
		case <-time.After(1200 * time.Millisecond):
		}
		s.out <- heard("done", true)
		nextHeard(t, l)
		select {
		case <-s.closed:
		case <-time.After(5 * time.Second):
			require.Fail(t, "an old stream wasn't ended once the sentence was")
		}
	})
	t.Run("retried, then given up", func(t *testing.T) {
		l, f := testListener(t)
		hearQuiet = time.Hour
		// talk sends audio until a stream opens for it (a stream that's
		// failing may take some with it).
		talk := func() *fakeRecognize {
			t.Helper()
			for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
				l.Send(make([]byte, 320))
				select {
				case s := <-f.opened:
					return s
				case <-time.After(100 * time.Millisecond):
				}
			}
			require.Fail(t, "no stream opened")
			return nil
		}
		unavailable := status.Error(codes.Unavailable, "try again")
		talk().err <- unavailable
		// A stream that works in between starts the count again.
		ok := talk()
		ok.out <- heard("fine", true)
		nextHeard(t, l)
		l.Flush()
		<-ok.closed
		for range hearRetries {
			talk().err <- unavailable
		}
		e := nextHeard(t, l)
		assert.Equal(t, HearError, e.Kind)
		assert.Equal(t, codes.Unavailable, status.Code(e.Err))
		_, open := <-l.Events()
		assert.False(t, open, "the listener has ended")
	})
	t.Run("not a stream's fault: at once", func(t *testing.T) {
		l, f := testListener(t)
		l.Send(make([]byte, 320))
		nextStream(t, f).err <- status.Error(codes.PermissionDenied, "SERVICE_DISABLED")
		assert.Equal(t, codes.PermissionDenied, status.Code(nextHeard(t, l).Err))
	})
	t.Run("no stream to be had", func(t *testing.T) {
		l, f := testListener(t)
		f.fail = errors.New(`auth: "invalid_grant"`)
		l.Send(make([]byte, 320))
		assert.ErrorContains(t, nextHeard(t, l).Err, "invalid_grant")
	})
	t.Run("more audio than it can keep is dropped, not waited on", func(t *testing.T) {
		quickHearing(t)
		l := &listener{audio: make(chan []byte, 1), flush: make(chan struct{}, 1)}
		l.Send(make([]byte, 3*maxHearChunk))
		assert.Len(t, l.audio, 1)
		l.Flush()
		l.Flush()
		assert.Len(t, l.flush, 1)
	})
}

// What Google says is said in words to act on (VOX-74).
func TestCloudVoiceProblem(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"none", nil, ""},
		{"a sign-in run out", errors.New(`transport: per-RPC creds failed due to error: auth: "invalid_grant" "Bad Request"`), "sign in again with `gcloud auth application-default login`"},
		{"unauthenticated", status.Error(codes.Unauthenticated, "no"), "sign in again"},
		{"Text-to-Speech off", status.Error(codes.PermissionDenied, "Cloud Text-to-Speech API has not been used in project p before or it is disabled. texttospeech.googleapis.com"), "Google Cloud's Text-to-Speech API isn't turned on in the project p"},
		{"Speech-to-Text off", status.Error(codes.PermissionDenied, "SERVICE_DISABLED speech.googleapis.com"), "Google Cloud's Speech-to-Text API isn't turned on in the project p"},
		{"refused", status.Error(codes.PermissionDenied, "caller lacks permission"), "Google Cloud refused the voice in the project p"},
		{"out of quota", status.Error(codes.ResourceExhausted, "quota"), "quota for speech is spent"},
		{"a model that isn't there", status.Error(codes.InvalidArgument, "The model \"nope\" does not exist in the location named \"global\"."), "doesn't take the voice's settings (its locale, or Speech-to-Text's model or location): The model \"nope\" does not exist"},
		{"something else", errors.New("connection reset"), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := CloudVoiceProblem(tc.err, "p")
			if tc.want == "" {
				assert.Empty(t, got)
				return
			}
			assert.Contains(t, got, tc.want)
		})
	}
	assert.NotContains(t, CloudVoiceProblem(status.Error(codes.PermissionDenied, "no"), ""), "in the project")
}

// The cloud engine's access (VOX-70): Google Cloud's own credentials,
// tried when asked, and a project, the settings' before the
// credentials'; without either it isn't set up, saying which.
func TestCloudVoiceAccess(t *testing.T) {
	old := googleCredentials
	t.Cleanup(func() { googleCredentials = old })
	t.Setenv("GOOGLE_CLOUD_PROJECT", "")
	cfg := config.DefaultConfig()
	creds := func(token error, quota string) {
		googleCredentials = func() (*auth.Credentials, error) {
			return auth.NewCredentials(&auth.CredentialsOptions{
				TokenProvider:          tokens{token},
				QuotaProjectIDProvider: auth.CredentialsPropertyFunc(func(context.Context) (string, error) { return quota, nil }),
			}), nil
		}
	}
	ctx := context.Background()

	googleCredentials = func() (*auth.Credentials, error) { return nil, errors.New("none found") }
	_, err := CloudVoiceProject(ctx, cfg, false)
	assert.ErrorIs(t, err, ErrVoiceNotSetUp)
	assert.ErrorContains(t, err, "gcloud auth application-default login")

	creds(nil, "")
	_, err = CloudVoiceProject(ctx, cfg, false)
	assert.ErrorContains(t, err, "needs a Google Cloud project")

	creds(nil, "quota-project")
	p, err := CloudVoiceProject(ctx, cfg, true)
	require.NoError(t, err)
	assert.Equal(t, "quota-project", p, "the credentials' own")
	t.Setenv("GOOGLE_CLOUD_PROJECT", "env-project")
	p, _ = CloudVoiceProject(ctx, cfg, false)
	assert.Equal(t, "env-project", p)
	cfg.LLM.Gemini.ProjectID = "set-project"
	p, _ = CloudVoiceProject(ctx, cfg, false)
	assert.Equal(t, "set-project", p, "the settings' first")

	creds(errors.New(`auth: "invalid_grant" "Bad Request"`), "quota-project")
	_, err = CloudVoiceProject(ctx, cfg, false)
	assert.NoError(t, err, "not tried unless asked")
	_, err = CloudVoiceProject(ctx, cfg, true)
	assert.ErrorIs(t, err, ErrVoiceNotSetUp)
	assert.ErrorContains(t, err, "sign in again")
	_, err = NewCloudVoice(ctx, cfg)
	assert.ErrorIs(t, err, ErrVoiceNotSetUp, "nor is a voice built on a sign-in that's run out")

	// With a sign-in that works, the engine is built (nothing is asked of
	// Google until something is said or heard).
	creds(nil, "quota-project")
	cfg.Voice.HearLocation = "us-central1"
	cv, err := NewCloudVoice(ctx, cfg)
	require.NoError(t, err)
	assert.Equal(t, "set-project", cv.Project)
	l, err := cv.Listen(ctx, "en-GB")
	require.NoError(t, err)
	l.Close()
	cv.Close()

	assert.Equal(t, "en-GB-Chirp3-HD-Aoede", ChirpVoice("en-GB", "Aoede"))
	assert.Equal(t, "en-US-Chirp3-HD-Kore", ChirpVoice("", ""))
	fake := NewCloudVoiceWith(
		func(context.Context, string, config.VoiceTuning) (SpeechStream, error) {
			return nil, errors.New("spoke")
		},
		func(context.Context, string) (Listener, error) { return nil, errors.New("listened") })
	_, err = fake.Speak(ctx, "Kore", config.VoiceTuning{})
	assert.ErrorContains(t, err, "spoke")
	_, err = fake.Listen(ctx, "en-US")
	assert.ErrorContains(t, err, "listened")
	fake.Close()
}

// tokens is a token provider that answers, or fails with err.
type tokens struct{ err error }

func (p tokens) Token(context.Context) (*auth.Token, error) {
	if p.err != nil {
		return nil, p.err
	}
	return &auth.Token{Value: "t", Expiry: time.Now().Add(time.Hour)}, nil
}

// brokenRecognize is a stream whose sends fail after allow of them, and
// which says why when asked.
type brokenRecognize struct {
	mu    sync.Mutex
	allow int
	why   error
	ctx   context.Context
}

func (b *brokenRecognize) Send(*speechpb.StreamingRecognizeRequest) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.allow == 0 {
		return io.EOF
	}
	b.allow--
	return nil
}

func (b *brokenRecognize) CloseSend() error { return nil }

func (b *brokenRecognize) Recv() (*speechpb.StreamingRecognizeResponse, error) {
	b.mu.Lock()
	done := b.allow == 0
	b.mu.Unlock()
	if done {
		return nil, b.why
	}
	<-b.ctx.Done()
	return nil, status.Error(codes.Canceled, "cancelled")
}

// A send that fails says only that the stream has ended: why is the
// stream's to say, and that's what the listener reports (VOX-74),
// whether it's the settings, the first audio or audio after it that
// the stream refused.
func TestListenerSaysWhyAStreamEnded(t *testing.T) {
	refused := status.Error(codes.InvalidArgument, "The model \"nope\" does not exist")
	for name, allow := range map[string]int{"its settings refused": 0, "its first audio refused": 1, "audio after that refused": 2} {
		t.Run(name, func(t *testing.T) {
			quickHearing(t)
			hearQuiet = time.Hour
			l := newListener(context.Background(), hearing{model: "nope", language: "en-US", open: func(ctx context.Context) (recognizeStream, error) {
				return &brokenRecognize{allow: allow, why: refused, ctx: ctx}, nil
			}})
			defer l.Close()
			l.Send(make([]byte, 320))
			l.Send(make([]byte, 320))
			e := nextHeard(t, l)
			assert.Equal(t, HearError, e.Kind)
			assert.Equal(t, codes.InvalidArgument, status.Code(e.Err), "the stream's own account: %v", e.Err)
		})
	}
	// A stream that won't say why: the send's error stands.
	quickHearing(t)
	silent := &brokenRecognize{allow: 0, why: io.EOF, ctx: context.Background()}
	sendErr := errors.New("send failed")
	assert.Equal(t, sendErr, (&listener{}).recvError(silent, sendErr))
}
