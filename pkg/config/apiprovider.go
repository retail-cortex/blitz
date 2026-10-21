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

// The api provider (spec_models_015 §3.4): a gateway that speaks OpenAI's
// Responses API but signs requests with its own headers, which may carry
// a short-lived token printed by a script.

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

// APIConfig holds settings for the api provider: a gateway that speaks
// OpenAI's Responses API (requests go to <base_url>/responses), signed
// with the headers given here.
type APIConfig struct {
	// BaseURL is the gateway's address; requests go to <base_url>/responses.
	// {{TOKEN_SCRIPT}} may appear in its path or query, never its host.
	BaseURL string `toml:"base_url"`
	// Model is the model to use.
	Model string `toml:"model"`
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

// Check reports why the settings can't build a model (spec_models_015
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
	// Value is given only when it is exactly TokenPlaceholder: a header can
	// hold a credential, so other values never leave the settings file.
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
}

// describeAPI is the api provider's settings in a scope's file.
func describeAPI(raw map[string]any) APIProviderInfo {
	info := APIProviderInfo{
		BaseURL:             str(lookup(raw, "llm", "api", "base_url")),
		TokenScript:         str(lookup(raw, "llm", "api", "token_script")),
		TokenScriptLanguage: str(lookup(raw, "llm", "api", "token_script_language")),
	}
	headers := fileHeaders(raw)
	for _, name := range sortedKeys(headers) {
		h := APIHeaderInfo{Name: name, HasValue: headers[name] != ""}
		if headers[name] == TokenPlaceholder {
			h.Value = TokenPlaceholder
		}
		info.Headers = append(info.Headers, h)
	}
	return info
}

// fileHeaders are the [llm.api.headers] a scope's file sets.
func fileHeaders(raw map[string]any) map[string]string {
	table, _ := lookup(raw, "llm", "api", "headers").(map[string]any)
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
	}
	edits := []settingEdit{
		{table, "base_url", result.BaseURL},
		{table, "token_script", result.TokenScript},
		{table, "token_script_language", result.TokenScriptLanguage},
	}
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
