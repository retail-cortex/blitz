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
	"fmt"
	"slices"
	"strings"

	"github.com/retail-cortex/blitz/pkg/config"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/model/gemini"
	"google.golang.org/genai"
)

// The voice's model (spec_063_voice VOX-05 to VOX-07): a Gemini Live model
// through Blitz's gemini provider, its API key or Vertex AI, run by ADK's
// live runner (the model's genai client opens the Live API's socket).

// Live models when the provider lists none, by backend (VOX-06): the
// Gemini API's native-audio model and Vertex AI's.
const (
	DefaultVoiceModelGeminiAPI = "gemini-2.5-flash-native-audio-preview-09-2025"
	DefaultVoiceModelVertex    = "gemini-live-2.5-flash-preview-native-audio-09-2025"
)

// ErrVoiceNotSetUp is voice with no Google provider to use (VOX-03).
var ErrVoiceNotSetUp = errors.New("voice needs Google's Gemini: set up Gemini in Settings › Providers (an API key, or Vertex AI)")

// ErrVoiceModel is a [voice] model that isn't a Gemini Live model.
var ErrVoiceModel = errors.New("[voice] model isn't a Gemini Live model")

// VoiceModel is the voice's model and how it's reached.
type VoiceModel struct {
	model.LLM
	// ID is the model's name ("gemini-…"), Backend the API it's on.
	ID      string
	Backend genai.Backend
}

// liveClientConfig is cc ready for the Live API, which needs its API
// version named.
func liveClientConfig(cc *genai.ClientConfig) *genai.ClientConfig {
	if cc.HTTPOptions.APIVersion == "" {
		cc.HTTPOptions.APIVersion = "v1beta"
		if cc.Backend == genai.BackendVertexAI {
			cc.HTTPOptions.APIVersion = "v1beta1"
		}
	}
	return cc
}

// NewVoiceModel builds the voice's model from cfg (VOX-05, VOX-06): [voice]
// model ("gemini/<model>"), else the newest live, native-audio model the
// Gemini API lists, else the backend's default. ErrVoiceNotSetUp when
// Gemini can't sign in.
func NewVoiceModel(ctx context.Context, cfg *config.Config) (*VoiceModel, error) {
	cc, err := geminiClientConfig(ctx, cfg.LLM.Gemini, policyFrom(cfg.LLM))
	if err != nil {
		return nil, fmt.Errorf("%w (%v)", ErrVoiceNotSetUp, err)
	}
	cc = liveClientConfig(cc)
	name := ""
	if ref := strings.TrimSpace(cfg.Voice.Model); ref != "" {
		provider, n := ParseModelRef(ref, "gemini")
		if provider != "gemini" || n == "" {
			return nil, fmt.Errorf("%w: %q", ErrVoiceModel, ref)
		}
		name = n
	}
	if name == "" {
		name = defaultVoiceModel(ctx, cc)
	}
	m, err := gemini.NewModel(ctx, name, cc)
	if err != nil {
		return nil, err
	}
	return &VoiceModel{LLM: m, ID: name, Backend: cc.Backend}, nil
}

// VoiceBackend is how voice would reach Gemini ("gemini" for the Gemini
// API, "vertex" for Vertex AI), or why it can't (ErrVoiceNotSetUp,
// VOX-03). It asks nothing of Google.
func VoiceBackend(ctx context.Context, cfg *config.Config) (string, error) {
	cc, err := geminiClientConfig(ctx, cfg.LLM.Gemini, policyFrom(cfg.LLM))
	if err != nil {
		return "", fmt.Errorf("%w (%v)", ErrVoiceNotSetUp, err)
	}
	if cc.Backend == genai.BackendVertexAI {
		return "vertex", nil
	}
	return "gemini", nil
}

// VoiceModels are the live models the provider lists, newest first
// (VOX-41), and the one a conversation would use.
func VoiceModels(ctx context.Context, cfg *config.Config) ([]string, string, error) {
	cc, err := geminiClientConfig(ctx, cfg.LLM.Gemini, policyFrom(cfg.LLM))
	if err != nil {
		return nil, "", fmt.Errorf("%w (%v)", ErrVoiceNotSetUp, err)
	}
	cc = liveClientConfig(cc)
	def := defaultVoiceModel(ctx, cc)
	if cc.Backend == genai.BackendVertexAI {
		return []string{DefaultVoiceModelVertex}, def, nil
	}
	client, err := genai.NewClient(ctx, cc)
	if err != nil {
		return nil, def, err
	}
	var live []string
	for m, err := range client.Models.All(ctx) {
		if err != nil {
			return live, def, err
		}
		if slices.Contains(m.SupportedActions, "bidiGenerateContent") {
			live = append(live, strings.TrimPrefix(m.Name, "models/"))
		}
	}
	slices.Sort(live)
	slices.Reverse(live)
	return live, def, nil
}

// defaultVoiceModel is the newest model the Gemini API lists for live,
// two-way audio whose name says native audio, else any live one, else the
// backend's default (Vertex AI lists no actions).
func defaultVoiceModel(ctx context.Context, cc *genai.ClientConfig) string {
	fallback := DefaultVoiceModelGeminiAPI
	if cc.Backend == genai.BackendVertexAI {
		return DefaultVoiceModelVertex
	}
	client, err := genai.NewClient(ctx, cc)
	if err != nil {
		return fallback
	}
	var live, native []string
	for m, err := range client.Models.All(ctx) {
		if err != nil {
			break
		}
		if !slices.Contains(m.SupportedActions, "bidiGenerateContent") {
			continue
		}
		id := strings.TrimPrefix(m.Name, "models/")
		live = append(live, id)
		if strings.Contains(id, "native-audio") {
			native = append(native, id)
		}
	}
	for _, list := range [][]string{native, live} {
		if len(list) > 0 {
			slices.Sort(list)
			return list[len(list)-1]
		}
	}
	return fallback
}

// VoiceLiveConfig is the live connection's settings (VOX-07): audio
// answers in voice, both sides transcribed, Gemini's own activity
// detection unless hold, when the page says when the person speaks, and
// resumption, so a connection the server cycles carries on.
func VoiceLiveConfig(voice string, hold bool) agent.LiveRunConfig {
	cfg := agent.LiveRunConfig{
		ResponseModalities: []genai.Modality{genai.ModalityAudio},
		SpeechConfig: &genai.SpeechConfig{VoiceConfig: &genai.VoiceConfig{
			PrebuiltVoiceConfig: &genai.PrebuiltVoiceConfig{VoiceName: voice},
		}},
		InputAudioTranscription:  &genai.AudioTranscriptionConfig{},
		OutputAudioTranscription: &genai.AudioTranscriptionConfig{},
		SessionResumption:        &genai.SessionResumptionConfig{},
	}
	if hold {
		cfg.RealtimeInputConfig = &genai.RealtimeInputConfig{AutomaticActivityDetection: &genai.AutomaticActivityDetection{Disabled: true}}
	}
	return cfg
}
