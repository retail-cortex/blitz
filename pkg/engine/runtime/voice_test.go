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
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

// listing serves Gemini's model list.
func listing(t *testing.T, models string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !strings.Contains(r.URL.Path, "/models") {
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, `{"models":[`+models+`]}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GOOGLE_GEMINI_BASE_URL", srv.URL)
}

// noGoogle leaves Gemini with no way to sign in.
func noGoogle(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("CLOUDSDK_CONFIG", t.TempDir())
}

const liveModels = `{"name":"models/gemini-2.5-flash","supportedGenerationMethods":["generateContent"]},
	{"name":"models/gemini-live-2.5-flash-preview","supportedGenerationMethods":["bidiGenerateContent"]},
	{"name":"models/gemini-2.5-flash-native-audio-preview-09-2025","supportedGenerationMethods":["bidiGenerateContent"]},
	{"name":"models/gemini-2.5-flash-native-audio-preview-12-2025","supportedGenerationMethods":["bidiGenerateContent"]}`

// The voice's model (spec_063_voice VOX-05, VOX-06): [voice] model when
// set, else the newest native-audio model listed, else any live one, else
// the backend's default; a model that isn't Gemini's is refused; no way to
// sign in is "not set up".
func TestNewVoiceModel(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		models, model, want string
	}{
		"newest native audio": {liveModels, "", "gemini-2.5-flash-native-audio-preview-12-2025"},
		"any live":            {`{"name":"models/gemini-live-a","supportedGenerationMethods":["bidiGenerateContent"]},{"name":"models/gemini-live-b","supportedGenerationMethods":["bidiGenerateContent"]}`, "", "gemini-live-b"},
		"none listed":         {`{"name":"models/gemini-2.5-flash","supportedGenerationMethods":["generateContent"]}`, "", DefaultVoiceModelGeminiAPI},
		"set":                 {liveModels, "gemini/gemini-live-2.5-flash-preview", "gemini-live-2.5-flash-preview"},
	} {
		t.Run(name, func(t *testing.T) {
			noGoogle(t)
			t.Setenv("GEMINI_API_KEY", "a-key")
			listing(t, tc.models)
			cfg := config.DefaultConfig()
			cfg.Voice.Model = tc.model
			m, err := NewVoiceModel(ctx, cfg)
			require.NoError(t, err)
			assert.Equal(t, tc.want, m.ID)
			assert.Equal(t, genai.BackendGeminiAPI, m.Backend)
			assert.NotNil(t, m.LLM)
		})
	}

	noGoogle(t)
	t.Setenv("GEMINI_API_KEY", "a-key")
	listing(t, liveModels)
	cfg := config.DefaultConfig()
	cfg.Voice.Model = "openai/gpt-realtime"
	_, err := NewVoiceModel(ctx, cfg)
	assert.ErrorIs(t, err, ErrVoiceModel)

	models, def, err := VoiceModels(ctx, config.DefaultConfig())
	require.NoError(t, err)
	assert.Equal(t, []string{"gemini-live-2.5-flash-preview", "gemini-2.5-flash-native-audio-preview-12-2025", "gemini-2.5-flash-native-audio-preview-09-2025"}, models, "live ones, newest first")
	assert.Equal(t, "gemini-2.5-flash-native-audio-preview-12-2025", def)
	backend, err := VoiceBackend(ctx, config.DefaultConfig())
	require.NoError(t, err)
	assert.Equal(t, "gemini", backend)

	noGoogle(t)
	_, err = NewVoiceModel(ctx, config.DefaultConfig())
	assert.ErrorIs(t, err, ErrVoiceNotSetUp)
	_, err = VoiceBackend(ctx, config.DefaultConfig())
	assert.ErrorIs(t, err, ErrVoiceNotSetUp)
	_, _, err = VoiceModels(ctx, config.DefaultConfig())
	assert.ErrorIs(t, err, ErrVoiceNotSetUp)
}

// The Live API wants its version named, per backend; one named stays.
func TestLiveClientConfig(t *testing.T) {
	assert.Equal(t, "v1beta", liveClientConfig(&genai.ClientConfig{Backend: genai.BackendGeminiAPI}).HTTPOptions.APIVersion)
	assert.Equal(t, "v1beta1", liveClientConfig(&genai.ClientConfig{Backend: genai.BackendVertexAI}).HTTPOptions.APIVersion)
	assert.Equal(t, "v1alpha", liveClientConfig(&genai.ClientConfig{HTTPOptions: genai.HTTPOptions{APIVersion: "v1alpha"}}).HTTPOptions.APIVersion)
	assert.Equal(t, DefaultVoiceModelVertex, defaultVoiceModel(context.Background(), &genai.ClientConfig{Backend: genai.BackendVertexAI}))
}

// The live connection's settings (VOX-07): audio in the voice, both sides
// transcribed, resumable; hold to talk turns Gemini's activity detection
// off.
func TestVoiceLiveConfig(t *testing.T) {
	tap := VoiceLiveConfig("Puck", false)
	assert.Equal(t, []genai.Modality{genai.ModalityAudio}, tap.ResponseModalities)
	assert.Equal(t, "Puck", tap.SpeechConfig.VoiceConfig.PrebuiltVoiceConfig.VoiceName)
	assert.NotNil(t, tap.InputAudioTranscription)
	assert.NotNil(t, tap.OutputAudioTranscription)
	assert.NotNil(t, tap.SessionResumption)
	assert.Nil(t, tap.RealtimeInputConfig)
	hold := VoiceLiveConfig("Kore", true)
	assert.True(t, hold.RealtimeInputConfig.AutomaticActivityDetection.Disabled)
}
