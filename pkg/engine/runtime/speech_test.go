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
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

// fakeGemini answers each call with pcm, recording the calls.
type fakeGemini struct {
	calls []string
	cfgs  []*genai.GenerateContentConfig
	pcm   []byte
	mime  string
	err   error
}

func (f *fakeGemini) GenerateContent(_ context.Context, model string, contents []*genai.Content, cfg *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
	f.calls = append(f.calls, contents[0].Parts[0].Text)
	f.cfgs = append(f.cfgs, cfg)
	if f.err != nil {
		return nil, f.err
	}
	parts := []*genai.Part{{Text: "ignored"}}
	if f.pcm != nil {
		parts = append(parts, &genai.Part{InlineData: &genai.Blob{Data: f.pcm, MIMEType: f.mime}})
	}
	return &genai.GenerateContentResponse{
		Candidates:    []*genai.Candidate{{Content: &genai.Content{Parts: parts}}},
		UsageMetadata: &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 10, CandidatesTokenCount: 100, TotalTokenCount: 110},
	}, nil
}

func TestGeminiSpeaker(t *testing.T) {
	pcm := make([]byte, 48000) // a second at 24 kHz, 16-bit
	long := strings.Repeat("A sentence about gradients. ", 300)
	tests := []struct {
		name     string
		req      tools.SpeakRequest
		calls    int
		prefix   string
		voices   []string // the voices configured, in order
		speakers []string
	}{
		{"one voice", tools.SpeakRequest{Text: "Hello there."}, 1, "Hello", []string{"Kore"}, nil},
		{"a chosen voice", tools.SpeakRequest{Text: "Hi.", Voice: "Charon"}, 1, "Hi.", []string{"Charon"}, nil},
		{"long text in chunks", tools.SpeakRequest{Text: long}, 3, "A sentence", []string{"Kore"}, nil},
		{"two hosts", tools.SpeakRequest{Text: "Ana: Hi.\nBen: Hello.", Speakers: []config.SpeakerConfig{{Name: "Ana"}, {Name: "Ben", Voice: "Fenrir"}}},
			1, "TTS the following conversation between Ana and Ben:\nAna: Hi.", []string{"Kore", "Fenrir"}, []string{"Ana", "Ben"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeGemini{pcm: pcm, mime: "audio/L16;codec=pcm;rate=24000"}
			s := &geminiSpeaker{models: f, model: "gemini-tts", host: "h"}
			sp, err := s.Speak(context.Background(), tc.req)
			require.NoError(t, err)
			require.Len(t, f.calls, tc.calls)
			assert.True(t, strings.HasPrefix(f.calls[0], tc.prefix), "%q", f.calls[0])
			assert.Equal(t, []string{"AUDIO"}, f.cfgs[0].ResponseModalities)
			sc := f.cfgs[0].SpeechConfig
			if tc.speakers == nil {
				assert.Equal(t, tc.voices[0], sc.VoiceConfig.PrebuiltVoiceConfig.VoiceName)
			} else {
				require.Len(t, sc.MultiSpeakerVoiceConfig.SpeakerVoiceConfigs, 2)
				for i, v := range sc.MultiSpeakerVoiceConfig.SpeakerVoiceConfigs {
					assert.Equal(t, tc.speakers[i], v.Speaker)
					assert.Equal(t, tc.voices[i], v.VoiceConfig.PrebuiltVoiceConfig.VoiceName)
				}
			}
			assert.Equal(t, "audio/wav", sp.MIME)
			assert.Equal(t, "RIFF", string(sp.Data[:4]))
			assert.Equal(t, "WAVE", string(sp.Data[8:12]))
			assert.Equal(t, uint32(24000), binary.LittleEndian.Uint32(sp.Data[24:28]))
			assert.Equal(t, uint32(len(pcm)*tc.calls), binary.LittleEndian.Uint32(sp.Data[40:44]))
			assert.InDelta(t, float64(tc.calls), sp.Seconds, 0.001)
			assert.EqualValues(t, 100*tc.calls, sp.Usage.CandidatesTokenCount, "usage is summed")
		})
	}
	assert.Equal(t, "gemini/gemini-tts", (&geminiSpeaker{model: "gemini-tts"}).Model())
	assert.Equal(t, ".wav", (&geminiSpeaker{}).Ext())
}

func TestGeminiSpeakerFails(t *testing.T) {
	s := &geminiSpeaker{models: &fakeGemini{}, model: "m"}
	_, err := s.Speak(context.Background(), tools.SpeakRequest{Text: "x"})
	assert.ErrorContains(t, err, "without audio")
	s = &geminiSpeaker{models: &fakeGemini{err: errors.New("quota")}, model: "m"}
	sp, err := s.Speak(context.Background(), tools.SpeakRequest{Text: "x"})
	assert.ErrorContains(t, err, "quota")
	assert.NotNil(t, sp.Usage, "usage so far is still reported")
}

// fakeOpenAISpeech answers each call with the voice's name as "MP3" bytes.
type fakeOpenAISpeech struct {
	calls []openai.AudioSpeechNewParams
	err   error
}

func (f *fakeOpenAISpeech) New(_ context.Context, body openai.AudioSpeechNewParams, _ ...option.RequestOption) (*http.Response, error) {
	f.calls = append(f.calls, body)
	if f.err != nil {
		return nil, f.err
	}
	return &http.Response{Body: io.NopCloser(strings.NewReader("[" + body.Voice.OfString.Value + "]"))}, nil
}

func TestOpenAISpeaker(t *testing.T) {
	f := &fakeOpenAISpeech{}
	s := &openAISpeaker{speech: f, model: "gpt-4o-mini-tts", host: "api.openai.com"}
	sp, err := s.Speak(context.Background(), tools.SpeakRequest{
		Text:     "Ana: Hi.\nAna: Still me.\nBen: Hello.\nand more from Ben\nAna: Bye.",
		Speakers: []config.SpeakerConfig{{Name: "Ana", Voice: "nova"}, {Name: "Ben"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "[nova][ash][nova]", string(sp.Data), "turn by turn, each in its voice")
	require.Len(t, f.calls, 3)
	assert.Equal(t, "Hi.\nStill me.", f.calls[0].Input)
	assert.Equal(t, "Hello.\nand more from Ben", f.calls[1].Input)
	assert.Equal(t, openai.AudioSpeechNewParamsResponseFormatMP3, f.calls[0].ResponseFormat)
	assert.Equal(t, "gpt-4o-mini-tts", f.calls[0].Model)
	assert.Equal(t, "audio/mpeg", sp.MIME)
	assert.Equal(t, "openai/gpt-4o-mini-tts", s.Model())
	assert.Equal(t, ".mp3", s.Ext())
	assert.Equal(t, "api.openai.com", s.Host())

	one, err := s.Speak(context.Background(), tools.SpeakRequest{Text: "Plain."})
	require.NoError(t, err)
	assert.Equal(t, "[coral]", string(one.Data))

	_, err = (&openAISpeaker{speech: &fakeOpenAISpeech{err: errors.New("401")}}).Speak(context.Background(), tools.SpeakRequest{Text: "x"})
	assert.ErrorContains(t, err, "401")
}

func TestSpeechChunks(t *testing.T) {
	tests := []struct {
		name string
		text string
		max  int
		want []string
	}{
		{"fits", "Hello.", 10, []string{"Hello."}},
		{"empty", "  ", 10, nil},
		{"by line", "one two\nthree four\nfive", 16, []string{"one two", "three four\nfive"}},
		{"by sentence", "First one. Second one. Third.", 12, []string{"First one.", "Second one.", "Third."}},
		{"by word", "aaaa bbbb cccc", 9, []string{"aaaa bbbb", "cccc"}},
		{"a huge word", "abcdefghij", 4, []string{"abcd", "efgh", "ij"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := speechChunks(tc.text, tc.max)
			assert.Equal(t, tc.want, got)
			for _, c := range got {
				assert.LessOrEqual(t, utf8.RuneCountInString(c), tc.max)
			}
		})
	}
}

func TestPCMRate(t *testing.T) {
	assert.Equal(t, 16000, pcmRate("audio/L16;codec=pcm;rate=16000", 24000))
	assert.Equal(t, 24000, pcmRate("audio/L16", 24000))
}

func TestNewSpeaker(t *testing.T) {
	tests := []struct {
		name    string
		cfg     func(*config.Config)
		model   string
		host    string
		wantErr string
	}{
		{"none", func(c *config.Config) {}, "", "", ""},
		{"gemini", func(c *config.Config) {
			c.Audio.Model = "gemini/gemini-2.5-flash-preview-tts"
			c.LLM.Gemini.APIKey = "k"
		}, "gemini/gemini-2.5-flash-preview-tts", "generativelanguage.googleapis.com", ""},
		{"openai", func(c *config.Config) {
			c.Audio.Model = "openai/gpt-4o-mini-tts"
			c.LLM.OpenAI.APIKey = "sk"
			c.LLM.OpenAI.BaseURL = "https://proxy.example.com/v1"
		}, "openai/gpt-4o-mini-tts", "proxy.example.com", ""},
		{"a provider that can't speak", func(c *config.Config) { c.Audio.Model = "anthropic/claude-sonnet-5" }, "", "", "can't generate speech"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			tc.cfg(cfg)
			sp, err := NewSpeaker(context.Background(), cfg)
			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			if tc.model == "" {
				assert.Nil(t, sp)
				return
			}
			assert.Equal(t, tc.model, sp.Model())
			assert.Equal(t, tc.host, sp.Host())
		})
	}
}

func TestRecordSpeech(t *testing.T) {
	f := newEngineWith(t, fixtureOpts{})
	f.eng.RecordSpeech(context.Background(), "s", "gemini/gemini-2.5-flash-preview-tts",
		&genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 1_000_000, CandidatesTokenCount: 1_000_000})
	u := f.eng.Usage("s")
	assert.True(t, u.Priced)
	assert.InDelta(t, 10.50, u.CostUSD, 0.001, "priced by [pricing]")

	assert.Zero(t, u.LastPrompt, "speech isn't the conversation's prompt")

	f.eng.RecordSpeech(context.Background(), "t", "openai/gpt-4o-mini-tts", nil)
	assert.False(t, f.eng.Usage("t").Priced, "no usage reported: the cost is unknown")
}

func TestAudioOfAndUsage(t *testing.T) {
	assert.Nil(t, audioOf(nil))
	assert.Nil(t, audioOf(&genai.GenerateContentResponse{Candidates: []*genai.Candidate{nil, {}, {Content: &genai.Content{Parts: []*genai.Part{nil, {InlineData: &genai.Blob{}}}}}}}))
	sum := &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 1}
	addUsage(sum, nil)
	assert.EqualValues(t, 1, sum.PromptTokenCount)
}
