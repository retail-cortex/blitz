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
	"bytes"
	"cmp"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"google.golang.org/genai"
)

// Speech chunks: Gemini's speech models take a few thousand characters
// well at a time; OpenAI's take at most 4,096.
const (
	geminiSpeechChunk = 4000
	openAISpeechChunk = 4000
)

// Default voices, when [audio] voice and the call name none.
const (
	defaultGeminiVoice = "Kore"
	defaultOpenAIVoice = "coral"
)

// NewSpeaker builds the [audio] model's speaker (spec_models_015): a
// Gemini speech model (through the Gemini API or Vertex AI, as [llm.gemini]
// signs in) or an OpenAI one. nil without a model; an error for a provider
// that can't speak or credentials that don't work.
func NewSpeaker(ctx context.Context, cfg *config.Config) (tools.Speaker, error) {
	ref := strings.TrimSpace(cfg.Audio.Model)
	if ref == "" {
		return nil, nil
	}
	provider, name := ParseModelRef(ref, cfg.LLM.Provider)
	pol := policyFrom(cfg.LLM)
	switch provider {
	case "gemini":
		cc, err := geminiClientConfig(ctx, cfg.LLM.Gemini, pol)
		if err != nil {
			return nil, err
		}
		client, err := genai.NewClient(ctx, cc)
		if err != nil {
			return nil, err
		}
		host := "generativelanguage.googleapis.com"
		if cc.Backend == genai.BackendVertexAI {
			host = "aiplatform.googleapis.com"
		}
		if u, err := url.Parse(cc.HTTPOptions.BaseURL); err == nil && u.Host != "" {
			host = u.Hostname()
		}
		return &geminiSpeaker{models: client.Models, model: name, host: host}, nil
	case "openai":
		key, base, opts, err := openAIKey(ctx, cfg.LLM.OpenAI, provider, pol)
		if err != nil {
			return nil, err
		}
		base = cmp.Or(base, defaultOpenAIBaseURL)
		client := openai.NewClient(append([]option.RequestOption{option.WithAPIKey(key), option.WithBaseURL(base)}, opts...)...)
		host := "api.openai.com"
		if u, err := url.Parse(base); err == nil && u.Host != "" {
			host = u.Hostname()
		}
		return &openAISpeaker{speech: &client.Audio.Speech, model: name, host: host}, nil
	}
	return nil, fmt.Errorf("%s can't generate speech: set [audio] model to a Gemini or OpenAI speech model", ref)
}

// geminiModels is the part of genai's client a geminiSpeaker uses.
type geminiModels interface {
	GenerateContent(ctx context.Context, model string, contents []*genai.Content, config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error)
}

// geminiSpeaker speaks with a Gemini speech model, which answers with
// 16-bit PCM; the chunks' PCM is joined and given a WAV header.
type geminiSpeaker struct {
	models geminiModels
	model  string
	host   string
}

func (s *geminiSpeaker) Model() string { return "gemini/" + s.model }
func (s *geminiSpeaker) Host() string  { return s.host }
func (s *geminiSpeaker) Ext() string   { return ".wav" }

func (s *geminiSpeaker) Speak(ctx context.Context, req tools.SpeakRequest) (*tools.Speech, error) {
	sc := &genai.SpeechConfig{VoiceConfig: prebuilt(cmp.Or(req.Voice, defaultGeminiVoice))}
	prefix := ""
	if len(req.Speakers) == 2 {
		sc = &genai.SpeechConfig{MultiSpeakerVoiceConfig: &genai.MultiSpeakerVoiceConfig{}}
		for i, sp := range req.Speakers {
			voice := cmp.Or(sp.Voice, []string{defaultGeminiVoice, "Puck"}[i])
			sc.MultiSpeakerVoiceConfig.SpeakerVoiceConfigs = append(sc.MultiSpeakerVoiceConfig.SpeakerVoiceConfigs,
				&genai.SpeakerVoiceConfig{Speaker: sp.Name, VoiceConfig: prebuilt(voice)})
		}
		prefix = fmt.Sprintf("TTS the following conversation between %s and %s:\n", req.Speakers[0].Name, req.Speakers[1].Name)
	}
	var pcm bytes.Buffer
	rate := 24000
	usage := &genai.GenerateContentResponseUsageMetadata{}
	for _, chunk := range speechChunks(req.Text, geminiSpeechChunk) {
		resp, err := s.models.GenerateContent(ctx, s.model, genai.Text(prefix+chunk), &genai.GenerateContentConfig{
			ResponseModalities: []string{string(genai.ModalityAudio)},
			SpeechConfig:       sc,
		})
		if err != nil {
			return &tools.Speech{Usage: usage}, err
		}
		addUsage(usage, resp.UsageMetadata)
		blob := audioOf(resp)
		if blob == nil {
			return &tools.Speech{Usage: usage}, errors.New("the model answered without audio")
		}
		rate = pcmRate(blob.MIMEType, rate)
		pcm.Write(blob.Data)
	}
	return &tools.Speech{
		Data: wav(pcm.Bytes(), rate), MIME: "audio/wav",
		Seconds: float64(pcm.Len()) / float64(rate*2), Usage: usage,
	}, nil
}

func prebuilt(voice string) *genai.VoiceConfig {
	return &genai.VoiceConfig{PrebuiltVoiceConfig: &genai.PrebuiltVoiceConfig{VoiceName: voice}}
}

// audioOf is a response's first inline audio.
func audioOf(resp *genai.GenerateContentResponse) *genai.Blob {
	if resp == nil {
		return nil
	}
	for _, c := range resp.Candidates {
		if c == nil || c.Content == nil {
			continue
		}
		for _, p := range c.Content.Parts {
			if p != nil && p.InlineData != nil && len(p.InlineData.Data) > 0 {
				return p.InlineData
			}
		}
	}
	return nil
}

func addUsage(sum, u *genai.GenerateContentResponseUsageMetadata) {
	if u == nil {
		return
	}
	sum.PromptTokenCount += u.PromptTokenCount
	sum.CandidatesTokenCount += u.CandidatesTokenCount
	sum.TotalTokenCount += u.TotalTokenCount
}

var rateParam = regexp.MustCompile(`rate=(\d+)`)

// pcmRate is the sample rate in an audio/L16 media type ("audio/L16;
// codec=pcm;rate=24000"), else def.
func pcmRate(mime string, def int) int {
	if m := rateParam.FindStringSubmatch(mime); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			return n
		}
	}
	return def
}

// wav wraps 16-bit mono little-endian PCM in a WAV header.
func wav(pcm []byte, rate int) []byte {
	var b bytes.Buffer
	b.Grow(44 + len(pcm))
	w := func(v any) { binary.Write(&b, binary.LittleEndian, v) }
	b.WriteString("RIFF")
	w(uint32(36 + len(pcm)))
	b.WriteString("WAVEfmt ")
	w(uint32(16))       // fmt chunk size
	w(uint16(1))        // PCM
	w(uint16(1))        // mono
	w(uint32(rate))     // sample rate
	w(uint32(rate * 2)) // byte rate
	w(uint16(2))        // block align
	w(uint16(16))       // bits per sample
	b.WriteString("data")
	w(uint32(len(pcm)))
	b.Write(pcm)
	return b.Bytes()
}

// openAISpeech is the part of openai-go's client an openAISpeaker uses.
type openAISpeech interface {
	New(ctx context.Context, body openai.AudioSpeechNewParams, opts ...option.RequestOption) (*http.Response, error)
}

// openAISpeaker speaks with an OpenAI speech model, as MP3; a
// conversation is spoken turn by turn in each speaker's voice, and the
// MP3s joined (MP3 frames play one after another).
type openAISpeaker struct {
	speech openAISpeech
	model  string
	host   string
}

func (s *openAISpeaker) Model() string { return "openai/" + s.model }
func (s *openAISpeaker) Host() string  { return s.host }
func (s *openAISpeaker) Ext() string   { return ".mp3" }

func (s *openAISpeaker) Speak(ctx context.Context, req tools.SpeakRequest) (*tools.Speech, error) {
	var out bytes.Buffer
	for _, turn := range speakerTurns(req, openAISpeechChunk) {
		res, err := s.speech.New(ctx, openai.AudioSpeechNewParams{
			Input: turn.text, Model: s.model,
			Voice:          openai.AudioSpeechNewParamsVoiceUnion{OfString: openai.String(cmp.Or(turn.voice, defaultOpenAIVoice))},
			ResponseFormat: openai.AudioSpeechNewParamsResponseFormatMP3,
		})
		if err != nil {
			return nil, err
		}
		_, err = io.Copy(&out, res.Body)
		res.Body.Close()
		if err != nil {
			return nil, err
		}
	}
	return &tools.Speech{Data: out.Bytes(), MIME: "audio/mpeg"}, nil
}

type turn struct{ voice, text string }

// speakerTurns cuts a request into calls for a model that speaks in one
// voice at a time: chunks of the text in req.Voice, or for a
// conversation, runs of one speaker's lines in their voice (a line naming
// no speaker stays with the last).
func speakerTurns(req tools.SpeakRequest, max int) []turn {
	if len(req.Speakers) < 2 {
		var out []turn
		for _, c := range speechChunks(req.Text, max) {
			out = append(out, turn{req.Voice, c})
		}
		return out
	}
	voices := map[string]string{}
	for i, sp := range req.Speakers {
		voices[strings.ToLower(sp.Name)] = cmp.Or(sp.Voice, []string{defaultOpenAIVoice, "ash"}[i])
	}
	var out []turn
	voice := voices[strings.ToLower(req.Speakers[0].Name)]
	var cur strings.Builder
	flush := func() {
		if t := strings.TrimSpace(cur.String()); t != "" {
			for _, c := range speechChunks(t, max) {
				out = append(out, turn{voice, c})
			}
		}
		cur.Reset()
	}
	for line := range strings.Lines(req.Text) {
		if name, rest, ok := strings.Cut(line, ":"); ok {
			if v, known := voices[strings.ToLower(strings.TrimSpace(name))]; known {
				if v != voice {
					flush()
					voice = v
				}
				line = strings.TrimLeft(rest, " ")
			}
		}
		cur.WriteString(line)
	}
	flush()
	return out
}

var sentenceEnd = regexp.MustCompile(`[.!?…]["')\]]*\s+`)

// speechChunks splits text into pieces of at most max characters, at
// paragraph, then line, then sentence, then word boundaries, so no piece
// stops mid-thought and a conversation's lines stay whole.
func speechChunks(text string, max int) []string {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) <= max {
		if text == "" {
			return nil
		}
		return []string{text}
	}
	var out []string
	var cur strings.Builder
	n := 0
	add := func(piece string) {
		// Trailing spaces and newlines don't count: they're trimmed.
		k := utf8.RuneCountInString(strings.TrimRight(piece, " \n"))
		if n > 0 && n+k > max {
			out = append(out, strings.TrimSpace(cur.String()))
			cur.Reset()
			n = 0
		}
		cur.WriteString(piece)
		n = utf8.RuneCountInString(cur.String())
	}
	for _, unit := range splitUnits(text, max) {
		add(unit)
	}
	if t := strings.TrimSpace(cur.String()); t != "" {
		out = append(out, t)
	}
	return out
}

// splitUnits cuts text into lines (with their newline), and a line longer
// than max into sentences, and a sentence longer than max into words.
func splitUnits(text string, max int) []string {
	var units []string
	for line := range strings.Lines(text) {
		if utf8.RuneCountInString(line) <= max {
			units = append(units, line)
			continue
		}
		last := 0
		var sentences []string
		for _, m := range sentenceEnd.FindAllStringIndex(line, -1) {
			sentences = append(sentences, line[last:m[1]])
			last = m[1]
		}
		sentences = append(sentences, line[last:])
		for _, s := range sentences {
			if utf8.RuneCountInString(s) <= max {
				units = append(units, s)
				continue
			}
			for _, w := range strings.SplitAfter(s, " ") {
				for utf8.RuneCountInString(w) > max { // one enormous "word"
					r := []rune(w)
					units = append(units, string(r[:max]))
					w = string(r[max:])
				}
				units = append(units, w)
			}
		}
	}
	return units
}
