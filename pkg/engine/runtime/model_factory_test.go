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
	"testing"
	"time"

	"cloud.google.com/go/auth"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

// staticToken is a token provider for tests.
type staticToken string

func (s staticToken) Token(context.Context) (*auth.Token, error) {
	return &auth.Token{Value: string(s), Type: "Bearer"}, nil
}

// fakeADC makes googleCredentials return a fixed token and quota project,
// or err.
func fakeADC(t *testing.T, err error) {
	t.Helper()
	old := googleCredentials
	t.Cleanup(func() { googleCredentials = old })
	googleCredentials = func() (*auth.Credentials, error) {
		if err != nil {
			return nil, err
		}
		return auth.NewCredentials(&auth.CredentialsOptions{
			TokenProvider:          staticToken("adc-token"),
			QuotaProjectIDProvider: auth.CredentialsPropertyFunc(func(context.Context) (string, error) { return "quota-p", nil }),
		}), nil
	}
}

// Gemini goes to the Gemini API with a key, or to Vertex AI with
// Application Default Credentials.
func TestGeminiClientConfig(t *testing.T) {
	pol := retryPolicy{maxRetries: 1, stall: time.Minute}
	fakeADC(t, nil)
	for _, tc := range []struct {
		name     string
		gemini   config.GeminiConfig
		env      map[string]string
		backend  genai.Backend
		apiKey   string
		project  string
		location string
		err      string
	}{
		{name: "an API key", gemini: config.GeminiConfig{APIKey: "AIza"}, backend: genai.BackendGeminiAPI, apiKey: "AIza"},
		{name: "an API key, said so", gemini: config.GeminiConfig{Auth: config.AuthAPIKey, APIKey: "AIza"}, backend: genai.BackendGeminiAPI, apiKey: "AIza"},
		{name: "no key leaves genai to the environment", gemini: config.GeminiConfig{ProjectID: "p"}, backend: genai.BackendUnspecified, project: "p"},
		{
			name:    "ADC with a project and location",
			gemini:  config.GeminiConfig{Auth: config.AuthADC, APIKey: "AIza-ignored", ProjectID: "p", Location: "us-central1"},
			backend: genai.BackendVertexAI, project: "p", location: "us-central1",
		},
		{
			name:    "ADC from the environment, in the global location",
			gemini:  config.GeminiConfig{Auth: config.AuthADC},
			env:     map[string]string{"GOOGLE_CLOUD_PROJECT": "env-p"},
			backend: genai.BackendVertexAI, project: "env-p", location: "global",
		},
		{name: "ADC without a project", gemini: config.GeminiConfig{Auth: config.AuthADC}, err: "needs a Google Cloud project"},
		{name: "an unknown method", gemini: config.GeminiConfig{Auth: "oauth"}, err: "unknown [llm.gemini] auth"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GOOGLE_CLOUD_PROJECT", tc.env["GOOGLE_CLOUD_PROJECT"])
			t.Setenv("GOOGLE_CLOUD_LOCATION", "")
			cc, err := geminiClientConfig(context.Background(), tc.gemini, pol)
			if tc.err != "" {
				assert.ErrorContains(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.backend, cc.Backend)
			assert.Equal(t, tc.apiKey, cc.APIKey)
			assert.Equal(t, tc.project, cc.Project)
			assert.Equal(t, tc.location, cc.Location)
		})
	}
}

// With ADC, requests carry the credentials' token and quota project, on
// Blitz's own HTTP client, which genai doesn't sign itself; credentials
// that can't be found fail when the model is built.
func TestGeminiADCSignsRequests(t *testing.T) {
	pol := retryPolicy{maxRetries: 0, stall: time.Minute}
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]}}]}`)
	}))
	defer srv.Close()

	fakeADC(t, nil)
	cc, err := geminiClientConfig(context.Background(), config.GeminiConfig{Auth: config.AuthADC, ProjectID: "p"}, pol)
	require.NoError(t, err)
	cc.HTTPOptions.BaseURL = srv.URL
	client, err := genai.NewClient(context.Background(), cc)
	require.NoError(t, err)
	res, err := client.Models.GenerateContent(context.Background(), "gemini-3.8-flash", genai.Text("hi"), nil)
	require.NoError(t, err)
	assert.Equal(t, "ok", res.Text())
	assert.Equal(t, "Bearer adc-token", got.Get("Authorization"))
	assert.Equal(t, "quota-p", got.Get("X-Goog-User-Project"))

	fakeADC(t, errors.New("could not find default credentials"))
	_, err = geminiClientConfig(context.Background(), config.GeminiConfig{Auth: config.AuthADC, ProjectID: "p"}, pol)
	assert.ErrorContains(t, err, "gcloud auth application-default login")
}
