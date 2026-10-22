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

package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validAPI is api provider settings that pass Check, for cases to break.
func validAPI() APIConfig {
	return APIConfig{
		BaseURL:     "https://gateway.example.com",
		Headers:     map[string]string{"X-Api-Key": TokenPlaceholder},
		TokenScript: "echo t",
	}
}

// Check refuses each of MDL-100's broken settings, naming the rule.
func TestAPIConfigCheck(t *testing.T) {
	cases := map[string]struct {
		edit func(*APIConfig)
		want string // "" for valid
	}{
		"valid":                    {func(*APIConfig) {}, ""},
		"static headers, no token": {func(a *APIConfig) { a.Headers = map[string]string{"X-Tenant": "t"}; a.TokenScript = "" }, ""},
		"token in the path":        {func(a *APIConfig) { a.BaseURL = "https://g.example.com/v1/" + TokenPlaceholder }, ""},
		"token in the query":       {func(a *APIConfig) { a.BaseURL = "https://g.example.com/?k=" + TokenPlaceholder }, ""},
		"python":                   {func(a *APIConfig) { a.TokenScriptLanguage = ScriptPython }, ""},
		"typescript":               {func(a *APIConfig) { a.TokenScriptLanguage = ScriptTypeScript }, ""},
		"no base URL":              {func(a *APIConfig) { a.BaseURL = "" }, "needs a base_url"},
		"relative URL":             {func(a *APIConfig) { a.BaseURL = "/responses" }, "isn't an absolute http or https URL"},
		"other scheme":             {func(a *APIConfig) { a.BaseURL = "ftp://g.example.com" }, "isn't an absolute http or https URL"},
		"token in the host":        {func(a *APIConfig) { a.BaseURL = "https://" + TokenPlaceholder + ".example.com" }, "not its host"},
		"token in the user":        {func(a *APIConfig) { a.BaseURL = "https://" + TokenPlaceholder + "@g.example.com" }, "not its host"},
		"no headers":               {func(a *APIConfig) { a.Headers = nil }, "at least one header"},
		"bad header name":          {func(a *APIConfig) { a.Headers["X Api"] = "v" }, "isn't a valid HTTP header name"},
		"same header twice":        {func(a *APIConfig) { a.Headers["x-api-key"] = "v" }, "are the same header"},
		"line break in a value":    {func(a *APIConfig) { a.Headers["X-Tenant"] = "a\r\nX-Evil: 1" }, "has a line break"},
		"placeholder, no script":   {func(a *APIConfig) { a.TokenScript = "  " }, "has no token_script"},
		"script nothing uses":      {func(a *APIConfig) { a.Headers = map[string]string{"X-Tenant": "t"} }, "no header or base_url uses"},
		"unknown language":         {func(a *APIConfig) { a.TokenScriptLanguage = "ruby" }, `token_script_language "ruby"`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			a := validAPI()
			tc.edit(&a)
			err := a.Check()
			if tc.want == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

// The language defaults to shell; the placeholder is found in headers and
// the URL.
func TestAPIConfigHelpers(t *testing.T) {
	assert.Equal(t, ScriptShell, APIConfig{}.Language())
	assert.Equal(t, ScriptPython, APIConfig{TokenScriptLanguage: ScriptPython}.Language())
	assert.False(t, APIConfig{Headers: map[string]string{"A": "b"}}.UsesToken())
	assert.True(t, APIConfig{Headers: map[string]string{"A": "Bearer " + TokenPlaceholder}}.UsesToken(), "inside a longer value")
	assert.True(t, APIConfig{BaseURL: "https://g/" + TokenPlaceholder}.UsesToken())
}

// SetProvider writes the api provider's settings; Describe shows them with
// header values withheld; a header saved without a value keeps its own.
func TestSetProviderAPI(t *testing.T) {
	keysEnv(t)
	save := func(s APIProviderSettings) error {
		_, err := SetProvider("", "", ProviderChoice{Provider: "api", Model: "gpt-5", API: &s})
		return err
	}
	require.NoError(t, save(APIProviderSettings{
		BaseURL:             " https://gateway.example.com ",
		TokenScript:         "curl -s https://auth.example.com/token\n| jq -r .token",
		TokenScriptLanguage: ScriptShell,
		Headers: []APIHeader{
			{Name: "X-Api-Key", Value: TokenPlaceholder},
			{Name: "X-Tenant", Value: "secret-tenant"},
			{Name: "X.Odd", Value: "quoted key"},
		},
	}))
	cfg, err := Load("")
	require.NoError(t, err)
	assert.Equal(t, "api", cfg.LLM.Provider)
	assert.Equal(t, "gpt-5", cfg.ModelName())
	assert.Equal(t, "https://gateway.example.com", cfg.LLM.API.BaseURL, "trimmed")
	assert.Equal(t, "curl -s https://auth.example.com/token\n| jq -r .token", cfg.LLM.API.TokenScript, "a multi-line script round-trips")
	assert.Equal(t, map[string]string{"X-Api-Key": TokenPlaceholder, "X-Tenant": "secret-tenant", "X.Odd": "quoted key"}, cfg.LLM.API.Headers)

	info, err := Describe("", "")
	require.NoError(t, err)
	assert.Equal(t, "https://gateway.example.com", info.API.BaseURL)
	assert.Equal(t, ScriptShell, info.API.TokenScriptLanguage)
	assert.Equal(t, []APIHeaderInfo{
		{Name: "X-Api-Key", Value: TokenPlaceholder, HasValue: true},
		{Name: "X-Tenant", HasValue: true},
		{Name: "X.Odd", HasValue: true},
	}, info.API.Headers, "only the placeholder is shown")

	t.Run("empty value keeps it, missing header is removed", func(t *testing.T) {
		require.NoError(t, save(APIProviderSettings{
			BaseURL:     "https://gateway.example.com",
			TokenScript: "echo t",
			Headers:     []APIHeader{{Name: "X-Api-Key", Value: TokenPlaceholder}, {Name: "X-Tenant"}},
		}))
		cfg, err := Load("")
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"X-Api-Key": TokenPlaceholder, "X-Tenant": "secret-tenant"}, cfg.LLM.API.Headers)
		assert.Empty(t, cfg.LLM.API.TokenScriptLanguage, "\"\" removes the language: shell")
	})

	refused := map[string]struct {
		choice ProviderChoice
		want   string
	}{
		"new header without a value": {ProviderChoice{Provider: "api", API: &APIProviderSettings{
			BaseURL: "https://g.example.com", Headers: []APIHeader{{Name: "X-New"}}}}, `header "X-New" needs a value`},
		"header twice": {ProviderChoice{Provider: "api", API: &APIProviderSettings{
			BaseURL: "https://g.example.com", Headers: []APIHeader{{Name: "A", Value: "1"}, {Name: "A", Value: "2"}}}}, "given twice"},
		"settings that can't build": {ProviderChoice{Provider: "api", API: &APIProviderSettings{
			BaseURL: "https://g.example.com", Headers: []APIHeader{{Name: "A", Value: TokenPlaceholder}}}}, "has no token_script"},
		"another provider": {ProviderChoice{Provider: "openai", API: &APIProviderSettings{}}, "need provider api"},
		"a key":            {ProviderChoice{Provider: "api", Key: "k"}, "takes an API key"},
	}
	for name, tc := range refused {
		t.Run(name, func(t *testing.T) {
			before, err := os.ReadFile(info.Path)
			require.NoError(t, err)
			_, err = SetProvider("", "", tc.choice)
			assert.ErrorContains(t, err, tc.want)
			after, err := os.ReadFile(info.Path)
			require.NoError(t, err)
			assert.Equal(t, string(before), string(after), "a refused change was written")
		})
	}
}

// The api provider is valid in llm.provider and named in its errors.
func TestAPIProviderIsValid(t *testing.T) {
	assert.True(t, ValidProvider("api"))
	keysEnv(t)
	_, err := SetValue("", "", "llm.provider", "nope")
	assert.ErrorContains(t, err, "vertex-anthropic or api")
	_, err = SetAPIKey("", "", "api", "k")
	assert.Error(t, err, "the api provider takes no key")
}
