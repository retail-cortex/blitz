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
	"cmp"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// The api provider (spec_015_models §3.4): a gateway that passes each
// model's requests on in the API it speaks (OpenAI's Responses or Chat
// Completions, Anthropic's Messages, Gemini's own or Vertex AI's), signed
// with its own headers, which may carry a short-lived token printed by a
// script.

// TokenPlaceholder, in an [llm.api] header's value or in its base URL's
// path or query, stands for the token script's output.
const TokenPlaceholder = "{{TOKEN_SCRIPT}}"

// The languages a token script can be written in.
const (
	ScriptShell      = "shell"
	ScriptPython     = "python"
	ScriptTypeScript = "typescript"
)

// TokenScriptLanguages are the languages a token script can be written in.
var TokenScriptLanguages = []string{ScriptShell, ScriptPython, ScriptTypeScript}

// The APIs a model behind the gateway can speak (spec_015_models
// MDL-112), as [llm.api.apis] names them.
const (
	// GatewayResponses is OpenAI's Responses API.
	GatewayResponses = "responses"
	// GatewayChatCompletions is OpenAI's Chat Completions API, which
	// OpenAI-compatible servers speak.
	GatewayChatCompletions = "chat_completions"
	// GatewayMessages is Anthropic's Messages API, streamed: Claude's, and
	// Anthropic-compatible servers'.
	GatewayMessages = "messages"
	// GatewayGemini is the Gemini API's generateContent.
	GatewayGemini = "gemini"
	// GatewayVertex is Vertex AI's generateContent for Google's models.
	GatewayVertex = "vertex"
)

// GatewayAPIs are the APIs a model behind the gateway can speak.
var GatewayAPIs = []string{GatewayResponses, GatewayChatCompletions, GatewayMessages, GatewayGemini, GatewayVertex}

// DefaultGatewayPaths are where each API's model requests go under
// base_url when [llm.api.paths] doesn't say (MDL-113). Claude's go where
// the others' do, as gateways that route by the request's model take them;
// {model} is the model's name and {method} Google's (generateContent, or
// streamGenerateContent).
var DefaultGatewayPaths = map[string]string{
	GatewayResponses:       "/responses",
	GatewayChatCompletions: "/chat/completions",
	GatewayMessages:        "/responses",
	GatewayGemini:          "/models/{model}:{method}",
	GatewayVertex:          "/publishers/google/models/{model}:{method}",
}

// DefaultModelsPath is where the gateway lists its models (MDL-114).
const DefaultModelsPath = "/models"

// APIConfig holds settings for the api provider: a gateway whose models
// each speak one of GatewayAPIs, signed with the headers given here.
type APIConfig struct {
	// BaseURL is the gateway's address; a model's requests go to the path
	// for its API under it (Paths). {{TOKEN_SCRIPT}} may appear in its
	// path or query, never its host.
	BaseURL string `toml:"base_url"`
	// Model is the model to use.
	Model string `toml:"model"`
	// Models are the models the menus list for it (spec_061_models
	// MOD-42); none: every model the gateway lists.
	Models []string `toml:"models"`
	// ModelsPath is where, under BaseURL, the gateway lists its models
	// (GET; default DefaultModelsPath); "none": it doesn't, and the menus
	// list Model and Models.
	ModelsPath string `toml:"models_path"`
	// APIs is the API each model speaks, by name; "*" is every other
	// model's. A model in neither is guessed from its name (APIOf).
	APIs map[string]string `toml:"apis"`
	// Paths are where each API's requests go under BaseURL, by API, in
	// place of DefaultGatewayPaths.
	Paths map[string]string `toml:"paths"`
	// Headers are set on every request, by name (at least one);
	// {{TOKEN_SCRIPT}} in a value is replaced by the token script's output.
	Headers map[string]string `toml:"headers"`
	// TokenScript prints the token {{TOKEN_SCRIPT}} stands for, on one
	// line. It runs on the service's machine when a session first needs a
	// token and again when the gateway rejects it.
	TokenScript string `toml:"token_script"`
	// TokenScriptLanguage is the script's language: shell (/bin/sh, the
	// default), python (python3) or typescript (node 22.6 or later).
	TokenScriptLanguage string `toml:"token_script_language"`
}

// NoModelsList is ModelsPath for a gateway that doesn't list its models.
const NoModelsList = "none"

// APIOf is the API model speaks behind the gateway (MDL-112): APIs says,
// else its "*", else the name: Claude's Messages, Gemini's own API, and
// OpenAI's Responses for any other.
func (a APIConfig) APIOf(model string) string {
	if api := cmp.Or(a.APIs[model], a.APIs["*"]); api != "" {
		return api
	}
	name := strings.ToLower(model)
	switch {
	case strings.HasPrefix(name, "claude"):
		return GatewayMessages
	case strings.HasPrefix(name, "gemini"):
		return GatewayGemini
	}
	return GatewayResponses
}

// PathOf is where api's requests go under base_url: Paths says, else
// DefaultGatewayPaths.
func (a APIConfig) PathOf(api string) string {
	return cmp.Or(a.Paths[api], DefaultGatewayPaths[api])
}

// ModelsURLPath is where under base_url the gateway lists its models, ""
// when it doesn't.
func (a APIConfig) ModelsURLPath() string {
	if a.ModelsPath == NoModelsList {
		return ""
	}
	return cmp.Or(a.ModelsPath, DefaultModelsPath)
}

// Language is the token script's language: shell when unset.
func (a APIConfig) Language() string { return cmp.Or(a.TokenScriptLanguage, ScriptShell) }

// UsesToken reports whether a header or the base URL uses TokenPlaceholder.
func (a APIConfig) UsesToken() bool {
	if strings.Contains(a.BaseURL, TokenPlaceholder) {
		return true
	}
	for _, v := range a.Headers {
		if strings.Contains(v, TokenPlaceholder) {
			return true
		}
	}
	return false
}

// headerNameRE is an HTTP header name: a token (RFC 9110 §5.6.2).
var headerNameRE = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]+$")

// urlSentinel stands in for the placeholder while a URL is parsed: it is
// valid anywhere in one, so where it lands says where the placeholder was.
const urlSentinel = "blitztokenscriptplaceholder"

// Check reports why the settings can't build a model (spec_015_models
// MDL-100), or nil.
func (a APIConfig) Check() error {
	if err := checkAPIBaseURL(a.BaseURL); err != nil {
		return err
	}
	if len(a.Headers) == 0 {
		return errors.New("[llm.api] needs at least one header (headers)")
	}
	seen := map[string]string{}
	for _, name := range sortedKeys(a.Headers) {
		if !headerNameRE.MatchString(name) {
			return fmt.Errorf("[llm.api] header %q isn't a valid HTTP header name", name)
		}
		if other, dup := seen[strings.ToLower(name)]; dup {
			return fmt.Errorf("[llm.api] headers %q and %q are the same header", other, name)
		}
		seen[strings.ToLower(name)] = name
		if strings.ContainsAny(a.Headers[name], "\r\n\x00") {
			return fmt.Errorf("[llm.api] header %q's value has a line break", name)
		}
	}
	switch uses, script := a.UsesToken(), strings.TrimSpace(a.TokenScript) != ""; {
	case uses && !script:
		return fmt.Errorf("[llm.api] uses %s but has no token_script", TokenPlaceholder)
	case script && !uses:
		return fmt.Errorf("[llm.api] has a token_script but no header or base_url uses %s", TokenPlaceholder)
	}
	if !slices.Contains(TokenScriptLanguages, a.Language()) {
		return fmt.Errorf("[llm.api] token_script_language %q isn't one of %s", a.TokenScriptLanguage, strings.Join(TokenScriptLanguages, ", "))
	}
	return a.checkRoutes()
}

// pathPlaceholderRE is a {…} in a path.
var pathPlaceholderRE = regexp.MustCompile(`\{[^}]*\}`)

// checkRoutes reports what's wrong with apis, paths and models_path
// (MDL-114–114).
func (a APIConfig) checkRoutes() error {
	for _, model := range sortedKeys(a.APIs) {
		if !slices.Contains(GatewayAPIs, a.APIs[model]) {
			return fmt.Errorf("[llm.api] apis: %q's API %q isn't one of %s", model, a.APIs[model], strings.Join(GatewayAPIs, ", "))
		}
	}
	for _, api := range sortedKeys(a.Paths) {
		path := a.Paths[api]
		if !slices.Contains(GatewayAPIs, api) {
			return fmt.Errorf("[llm.api] paths: %q isn't an API (%s)", api, strings.Join(GatewayAPIs, ", "))
		}
		if err := checkPath("paths."+api, path); err != nil {
			return err
		}
		google := api == GatewayGemini || api == GatewayVertex
		for _, ph := range pathPlaceholderRE.FindAllString(path, -1) {
			if ph != "{model}" && (ph != "{method}" || !google) {
				return fmt.Errorf("[llm.api] paths.%s: %s isn't something Blitz fills in (%s)", api, ph, map[bool]string{true: "{model}, {method}", false: "{model}"}[google])
			}
		}
		if google && !strings.Contains(path, "{method}") {
			return fmt.Errorf("[llm.api] paths.%s needs {method}: Google's API says by it whether the reply streams", api)
		}
	}
	if a.ModelsPath != "" && a.ModelsPath != NoModelsList {
		return checkPath("models_path", a.ModelsPath)
	}
	return nil
}

// checkPath reports why path can't go under base_url.
func checkPath(key, path string) error {
	switch {
	case !strings.HasPrefix(path, "/"):
		return fmt.Errorf("[llm.api] %s %q must start with /", key, path)
	case strings.ContainsAny(path, "?# \t\r\n"):
		return fmt.Errorf("[llm.api] %s %q is a path alone: no query, fragment or spaces", key, path)
	case strings.Contains(path, TokenPlaceholder):
		return fmt.Errorf("[llm.api] %s can't use %s: base_url and the headers can", key, TokenPlaceholder)
	}
	return nil
}

func checkAPIBaseURL(base string) error {
	if base == "" {
		return errors.New("[llm.api] needs a base_url")
	}
	u, err := url.Parse(strings.ReplaceAll(base, TokenPlaceholder, urlSentinel))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("[llm.api] base_url %q isn't an absolute http or https URL", base)
	}
	if strings.Contains(u.Host, urlSentinel) || strings.Contains(u.User.String(), urlSentinel) {
		return fmt.Errorf("[llm.api] base_url can use %s only in its path or query, not its host", TokenPlaceholder)
	}
	return nil
}

// APIHeaderInfo is one of the api provider's headers, as Describe
// reports it.
type APIHeaderInfo struct {
	Name string
	// Value is given only when it uses TokenPlaceholder (as UsesToken
	// checks, anywhere in it: "Bearer {{TOKEN_SCRIPT}}"), the token then
	// being the script's; a header can hold a credential, so other values
	// never leave the settings file.
	Value    string
	HasValue bool
}

// APIProviderInfo is the api provider's settings as one scope sets them,
// header values withheld (CFG-44).
type APIProviderInfo struct {
	BaseURL             string
	TokenScript         string
	TokenScriptLanguage string
	Headers             []APIHeaderInfo // sorted by name
	ModelsPath          string
	APIs                map[string]string // model → API (MDL-112)
	Paths               map[string]string // API → path (MDL-113)
}

// describeAPI is the api provider's settings in a scope's file.
func describeAPI(raw map[string]any) APIProviderInfo {
	info := APIProviderInfo{
		BaseURL:             str(lookup(raw, "llm", "api", "base_url")),
		TokenScript:         str(lookup(raw, "llm", "api", "token_script")),
		TokenScriptLanguage: str(lookup(raw, "llm", "api", "token_script_language")),
		ModelsPath:          str(lookup(raw, "llm", "api", "models_path")),
		APIs:                fileTable(raw, "apis"),
		Paths:               fileTable(raw, "paths"),
	}
	headers := fileHeaders(raw)
	for _, name := range sortedKeys(headers) {
		h := APIHeaderInfo{Name: name, HasValue: headers[name] != ""}
		if strings.Contains(headers[name], TokenPlaceholder) {
			h.Value = headers[name]
		}
		info.Headers = append(info.Headers, h)
	}
	return info
}

// fileHeaders are the [llm.api.headers] a scope's file sets.
func fileHeaders(raw map[string]any) map[string]string { return fileTable(raw, "headers") }

// fileTable is the [llm.api.<name>] table a scope's file sets, nil when
// it sets none.
func fileTable(raw map[string]any, name string) map[string]string {
	table, _ := lookup(raw, "llm", "api", name).(map[string]any)
	if len(table) == 0 {
		return nil
	}
	out := make(map[string]string, len(table))
	for k, v := range table {
		out[k] = str(v)
	}
	return out
}

// APIHeader is a header the provider form saves; Value "" keeps the value
// the scope's file has for it.
type APIHeader struct {
	Name  string
	Value string
}

// APIProviderSettings are the api provider's settings the form saves
// with SetProvider (CFG-45b).
type APIProviderSettings struct {
	BaseURL             string
	Headers             []APIHeader
	TokenScript         string
	TokenScriptLanguage string
	// ModelsPath, APIs and Paths replace the file's: an API or path left
	// out is removed (MDL-114–114).
	ModelsPath string
	APIs       map[string]string
	Paths      map[string]string
}

// edits checks the settings s makes of a scope's file (raw) as the model
// would, and returns the edits that write them.
func (s APIProviderSettings) edits(raw map[string]any) ([]settingEdit, error) {
	const table, headersTable = "llm.api", "llm.api.headers"
	stored := fileHeaders(raw)
	result := APIConfig{
		BaseURL:             strings.TrimSpace(s.BaseURL),
		TokenScript:         strings.TrimSpace(s.TokenScript),
		TokenScriptLanguage: strings.TrimSpace(s.TokenScriptLanguage),
		Headers:             map[string]string{},
		ModelsPath:          strings.TrimSpace(s.ModelsPath),
		APIs:                trimmedTable(s.APIs),
		Paths:               trimmedTable(s.Paths),
	}
	edits := []settingEdit{
		{table, "base_url", result.BaseURL},
		{table, "token_script", result.TokenScript},
		{table, "token_script_language", result.TokenScriptLanguage},
		{table, "models_path", result.ModelsPath},
	}
	edits = append(edits, tableEdits("llm.api.apis", fileTable(raw, "apis"), result.APIs)...)
	edits = append(edits, tableEdits("llm.api.paths", fileTable(raw, "paths"), result.Paths)...)
	for _, h := range s.Headers {
		name := strings.TrimSpace(h.Name)
		if _, dup := result.Headers[name]; dup {
			return nil, fmt.Errorf("[llm.api] header %q is given twice", name)
		}
		switch {
		case h.Value != "":
			result.Headers[name] = h.Value
			edits = append(edits, settingEdit{headersTable, name, h.Value})
		case stored[name] != "":
			result.Headers[name] = stored[name] // kept as it is: no edit
		default:
			return nil, fmt.Errorf("[llm.api] header %q needs a value", name)
		}
	}
	for _, name := range sortedKeys(stored) {
		if _, kept := result.Headers[name]; !kept {
			edits = append(edits, settingEdit{headersTable, name, ""})
		}
	}
	if err := result.Check(); err != nil {
		return nil, err
	}
	return edits, nil
}

// trimmedTable is t with its keys and values trimmed, and those left
// empty dropped.
func trimmedTable(t map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range t {
		if k, v = strings.TrimSpace(k), strings.TrimSpace(v); k != "" && v != "" {
			out[k] = v
		}
	}
	return out
}

// tableEdits write want as table, removing what stored has beyond it.
func tableEdits(table string, stored, want map[string]string) []settingEdit {
	var edits []settingEdit
	for _, k := range sortedKeys(want) {
		if stored[k] != want[k] {
			edits = append(edits, settingEdit{table, k, want[k]})
		}
	}
	for _, k := range sortedKeys(stored) {
		if _, kept := want[k]; !kept {
			edits = append(edits, settingEdit{table, k, ""})
		}
	}
	return edits
}
