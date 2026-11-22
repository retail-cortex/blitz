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
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
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

// answering makes these models answer a live check, and no others, with
// nothing kept from another test.
func answering(t *testing.T, models ...string) *[]string {
	t.Helper()
	old := probeLive
	var asked []string
	var mu sync.Mutex
	probeLive = func(_ context.Context, _ *genai.ClientConfig, m string, setup *genai.LiveConnectConfig) error {
		if setup == nil || setup.SpeechConfig == nil || setup.SessionResumption == nil || setup.OutputAudioTranscription == nil {
			return errors.New("checked without the conversation's settings")
		}
		mu.Lock()
		asked = append(asked, m)
		mu.Unlock()
		if slices.Contains(models, m) {
			return nil
		}
		return errors.New("models/" + m + " is not found for API version v1beta")
	}
	liveMu.Lock()
	liveCache = map[string]liveFound{}
	liveMu.Unlock()
	t.Cleanup(func() {
		probeLive = old
		liveMu.Lock()
		liveCache = map[string]liveFound{}
		liveMu.Unlock()
	})
	return &asked
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
	{"name":"models/gemini-2.5-flash-native-audio-preview-12-2025","supportedGenerationMethods":["bidiGenerateContent"]},
	{"name":"models/gemini-3.8-live","supportedGenerationMethods":["bidiGenerateContent"]},
	{"name":"models/gemini-3.8-live-extended-thinking","supportedGenerationMethods":["bidiGenerateContent"]}`

// The voice's model (spec_063_voice VOX-05, VOX-06, VOX-06a): [voice]
// model when set and it answers; else the best of the listed live models
// that answer for the key, by version, not by how names sort; a listed
// model the key can't use isn't offered; none answering says why.
func TestNewVoiceModel(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		answer      []string
		model, want string
	}{
		"native audio first":        {[]string{"gemini-live-2.5-flash-preview", "gemini-2.5-flash-native-audio-preview-12-2025", "gemini-3.8-live"}, "", "gemini-2.5-flash-native-audio-preview-12-2025"},
		"else the highest version":  {[]string{"gemini-live-2.5-flash-preview", "gemini-3.8-live"}, "", "gemini-3.8-live"},
		"not one the key can't use": {[]string{"gemini-live-2.5-flash-preview", "gemini-2.5-flash-native-audio-preview-09-2025"}, "", "gemini-2.5-flash-native-audio-preview-09-2025"},
		"native audio at a version": {[]string{"gemini-live-2.5-flash-preview", "gemini-2.5-flash-native-audio-preview-12-2025"}, "", "gemini-2.5-flash-native-audio-preview-12-2025"},
		"set, and it answers":       {[]string{"gemini-live-2.5-flash-preview"}, "gemini/gemini-live-2.5-flash-preview", "gemini-live-2.5-flash-preview"},
	} {
		t.Run(name, func(t *testing.T) {
			noGoogle(t)
			t.Setenv("GEMINI_API_KEY", "a-key")
			listing(t, liveModels)
			answering(t, tc.answer...)
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
	asked := answering(t, "gemini-3.8-live", "gemini-2.5-flash-native-audio-preview-12-2025")
	cfg := config.DefaultConfig()
	cfg.Voice.Model = "openai/gpt-realtime"
	_, err := NewVoiceModel(ctx, cfg)
	assert.ErrorIs(t, err, ErrVoiceModel)
	cfg.Voice.Model = "gemini/gemini-3.8-live-extended-thinking"
	_, err = NewVoiceModel(ctx, cfg)
	assert.ErrorContains(t, err, "doesn't answer", "a set model the key can't use is said at once")

	models, def, err := VoiceModels(ctx, config.DefaultConfig())
	require.NoError(t, err)
	assert.Equal(t, []string{"gemini-2.5-flash-native-audio-preview-12-2025", "gemini-3.8-live"}, models, "only those that answer, best first")
	assert.Equal(t, "gemini-2.5-flash-native-audio-preview-12-2025", def)
	n := len(*asked)
	_, _, err = VoiceModels(ctx, config.DefaultConfig())
	require.NoError(t, err)
	assert.Len(t, *asked, n, "kept: not checked again")
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

// None of the listed models answers, or none is listed: why, with the
// provider's reason, rather than a model that fails mid-conversation.
func TestNoLiveModelAnswers(t *testing.T) {
	ctx := context.Background()
	noGoogle(t)
	t.Setenv("GEMINI_API_KEY", "a-key")
	listing(t, liveModels)
	answering(t)
	_, _, err := VoiceModels(ctx, config.DefaultConfig())
	require.ErrorIs(t, err, ErrNoLiveModel)
	assert.Contains(t, err.Error(), "is not found for API version")
	_, err = NewVoiceModel(ctx, config.DefaultConfig())
	assert.ErrorIs(t, err, ErrNoLiveModel)

	listing(t, `{"name":"models/gemini-2.5-flash","supportedGenerationMethods":["generateContent"]}`)
	answering(t, "gemini-2.5-flash")
	_, _, err = VoiceModels(ctx, config.DefaultConfig())
	assert.ErrorContains(t, err, "lists no live models")
}

// The check sends the conversation's own settings, the voice among them.
func TestLiveSetup(t *testing.T) {
	s := liveSetup("Kore")
	assert.Equal(t, "Kore", s.SpeechConfig.VoiceConfig.PrebuiltVoiceConfig.VoiceName)
	assert.Equal(t, []genai.Modality{genai.ModalityAudio}, s.ResponseModalities)
	assert.NotNil(t, s.InputAudioTranscription)
	assert.NotNil(t, s.OutputAudioTranscription)
	assert.NotNil(t, s.SessionResumption)
}

// Live models in order of what they are: native audio, version, date,
// released before preview.
func TestLiveBetter(t *testing.T) {
	got := []string{
		"gemini-live-2.5-flash-preview",
		"gemini-2.5-flash-native-audio-preview-09-2025",
		"gemini-3.1-flash-live-preview",
		"gemini-2.5-flash-native-audio-preview-12-2025",
		"gemini-3.8-live",
		"gemini-3.1-flash-live",
	}
	slices.SortFunc(got, liveBetter)
	assert.Equal(t, []string{
		"gemini-2.5-flash-native-audio-preview-12-2025",
		"gemini-2.5-flash-native-audio-preview-09-2025",
		"gemini-3.8-live",
		"gemini-3.1-flash-live",
		"gemini-3.1-flash-live-preview",
		"gemini-live-2.5-flash-preview",
	}, got)
}

// On Vertex AI, which lists no actions, a model whose name says live or
// native audio is a candidate.
func TestVertexLiveCandidates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"models":[{"name":"publishers/google/models/gemini-2.5-pro"},{"name":"publishers/google/models/gemini-live-2.5-flash"},{"name":"publishers/google/models/gemini-2.5-flash-native-audio"}]}`)
	}))
	t.Cleanup(srv.Close)
	cc := &genai.ClientConfig{Backend: genai.BackendVertexAI, Project: "p", Location: "us-central1", HTTPOptions: genai.HTTPOptions{BaseURL: srv.URL}, Credentials: nil}
	cc.HTTPClient = srv.Client()
	got, err := liveCandidates(context.Background(), cc)
	if err != nil {
		t.Skipf("the SDK wants credentials for Vertex AI here: %v", err)
	}
	assert.Equal(t, []string{"gemini-2.5-flash-native-audio", "gemini-live-2.5-flash"}, got)
}

// The Live API wants its version named, per backend; one named stays.
func TestLiveClientConfig(t *testing.T) {
	assert.Equal(t, "v1beta", liveClientConfig(&genai.ClientConfig{Backend: genai.BackendGeminiAPI}).HTTPOptions.APIVersion)
	assert.Equal(t, "v1beta1", liveClientConfig(&genai.ClientConfig{Backend: genai.BackendVertexAI}).HTTPOptions.APIVersion)
	assert.Equal(t, "v1alpha", liveClientConfig(&genai.ClientConfig{HTTPOptions: genai.HTTPOptions{APIVersion: "v1alpha"}}).HTTPOptions.APIVersion)
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

// fakeLive serves the Live API's socket: it reads the setup and accepts it
// (setupComplete), or closes with refuse as the reason, as Google does for
// a model the key can't use. The setup it read is in got.
func fakeLive(t *testing.T, refuse string) (*genai.ClientConfig, *[]byte) {
	t.Helper()
	var got []byte
	up := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		_, msg, err := c.ReadMessage()
		if err != nil {
			return
		}
		got = msg
		if refuse != "" {
			_ = c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, refuse))
			return
		}
		_ = c.WriteMessage(websocket.TextMessage, []byte(`{"setupComplete":{}}`))
		_, _, _ = c.ReadMessage() // until the client closes
	}))
	t.Cleanup(srv.Close)
	cc := &genai.ClientConfig{Backend: genai.BackendGeminiAPI, APIKey: "a-key",
		HTTPOptions: genai.HTTPOptions{BaseURL: "ws://" + strings.TrimPrefix(srv.URL, "http://") + "/", APIVersion: "v1beta"}}
	return cc, &got
}

// The live check against a socket: accepted is an answer, the setup sent
// is the conversation's; refused is the provider's reason (the "failed to
// receive setup complete" a conversation stopped with).
func TestProbeLive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cc, got := fakeLive(t, "")
	require.NoError(t, probeLive(ctx, cc, "gemini-3.8-live", liveSetup("Kore")))
	assert.Contains(t, string(*got), "gemini-3.8-live")
	assert.Contains(t, string(*got), "Kore", "with the conversation's voice")
	assert.Contains(t, string(*got), "sessionResumption")

	cc, _ = fakeLive(t, "models/gemini-3.8-live is not found for API version v1beta")
	err := probeLive(ctx, cc, "gemini-3.8-live", liveSetup("Kore"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not found for API version v1beta", "Google's reason, kept")
}
