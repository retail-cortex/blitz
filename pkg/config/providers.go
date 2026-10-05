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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/retail-cortex/blitz/pkg/secrets"
	"github.com/rrmcguinness/modenv/pkg/modenv"
)

// Settings come in two scopes: the global file (~/.blitz/.env.toml) and,
// for each workspace, an overlay kept in the user's own directory
// (~/.blitz/workspaces/<name>-<hash>/.env.toml), never in the workspace:
// a repository can't change or redirect the user's keys. API keys are
// stored in the OS keychain and the files hold "keychain:<name>" references
// (package secrets); config resolves them when it loads.

// KeyedProviders are the providers that take an API key.
var KeyedProviders = []string{"gemini", "anthropic", "openai"}

// CloudProviders are the providers whose credentials come from their
// cloud's chain: Claude on Bedrock and on Vertex AI, and models on Azure.
var CloudProviders = []string{"bedrock", "azure", "vertex-anthropic"}

// ValidProvider reports whether p is a provider Blitz can use.
func ValidProvider(p string) bool {
	return p == "ollama" || knownProvider(p) || slices.Contains(CloudProviders, p)
}

// providerList names the providers, for errors.
const providerList = "gemini, anthropic, openai, ollama, bedrock, azure or vertex-anthropic"

// keyEnv are the environment variables each provider's key can come from.
var keyEnv = map[string][]string{
	"gemini":    {"GEMINI_API_KEY", "GOOGLE_API_KEY"},
	"anthropic": {"ANTHROPIC_API_KEY"},
	"openai":    {"OPENAI_API_KEY"},
}

// WorkspaceSettingsDir is where the workspace's own settings live, inside
// the global settings directory (prefixDir as for Load): readable
// ("shop-1a2b3c4d") and unique per directory.
func WorkspaceSettingsDir(prefixDir, workspace string) string {
	root := ConfigDir(prefixDir)
	if root == "" || workspace == "" {
		return ""
	}
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	sum := sha256.Sum256([]byte(abs))
	name := regexp.MustCompile(`[^A-Za-z0-9._-]+`).ReplaceAllString(filepath.Base(abs), "-")
	return filepath.Join(root, "workspaces", name+"-"+hex.EncodeToString(sum[:4]))
}

// LoadWorkspace is Load with the workspace's own settings over the global
// ones. It doesn't set Tools.WorkspaceDir: the caller does.
func LoadWorkspace(prefixDir, workspace string) (*Config, error) {
	return load(prefixDir, workspace)
}

// overlay decodes the workspace's settings file over cfg, if there is one.
func overlay(cfg *Config, prefixDir, workspace string) error {
	dir := WorkspaceSettingsDir(prefixDir, workspace)
	if dir == "" {
		return nil
	}
	path := filepath.Join(dir, ".env.toml")
	// A list the workspace sets would replace the global one: its
	// permission rules add to the global rules instead.
	global := cfg.Permissions
	cfg.Permissions = PermissionsConfig{}
	if _, err := toml.DecodeFile(path, cfg); err != nil && !errors.Is(err, os.ErrNotExist) {
		cfg.Permissions = global
		return fmt.Errorf("the workspace's settings (%s): %w", path, err)
	}
	cfg.Permissions = global.Merge(cfg.Permissions)
	return nil
}

// resolveSecrets replaces keychain references (and modenv's xor: values,
// from an overlay) in the API keys with the secrets themselves. A secret
// that can't be read leaves the key empty, so the environment or the
// model's own error takes over.
func resolveSecrets(cfg *Config, store secrets.Store) {
	for _, p := range []*string{&cfg.LLM.Gemini.APIKey, &cfg.LLM.Anthropic.APIKey, &cfg.LLM.OpenAI.APIKey} {
		if name, ok := secrets.ParseRef(*p); ok {
			v, err := store.Get(name)
			if err != nil {
				v = ""
			}
			*p = v
		} else if strings.HasPrefix(*p, "xor:") {
			if v, err := modenv.DecryptSecret(*p); err == nil {
				*p = v
			}
		}
	}
}

// KeySource says where a provider's key comes from.
type KeySource string

// Where a key can come from, as Describe reports it.
const (
	KeyNone        KeySource = "none"
	KeyKeychain    KeySource = "keychain"    // a reference to the OS store
	KeyPlain       KeySource = "plain"       // written in the file as is
	KeyObfuscated  KeySource = "obfuscated"  // modenv's xor: (not encryption)
	KeyEnvironment KeySource = "environment" // an environment variable
	KeyInherited   KeySource = "inherited"   // a workspace using the global key
)

// ProviderInfo describes a provider's settings in one scope, without its key.
type ProviderInfo struct {
	Name      string
	KeySource KeySource
	// KeyMissing means the file names a stored secret that isn't there.
	KeyMissing bool
	BaseURL    string
	Model      string
	// Auth is how the provider signs in, as set in this scope ("" when not:
	// an API key, or a workspace following the global setting), with its
	// Vertex AI project and location (adc) or ant profile (oauth).
	Auth      string
	ProjectID string
	Location  string
	Profile   string
}

// ScopeInfo describes the settings in one scope (workspace "" is global).
type ScopeInfo struct {
	Path         string // the settings file (it may not exist yet)
	Provider     string // llm.provider as set in this scope ("" if not)
	DefaultModel string // blitz.default_model as set in this scope
	Providers    []ProviderInfo
	SecretStore  string // where keys go
}

// Describe reports the settings of a scope: what the file sets, and for
// each provider where its key comes from.
func Describe(prefixDir, workspace string) (ScopeInfo, error) {
	dir := scopeDir(prefixDir, workspace)
	if dir == "" {
		return ScopeInfo{}, errors.New("no settings directory")
	}
	path := filepath.Join(dir, ".env.toml")
	raw, err := readTOML(path)
	if err != nil {
		return ScopeInfo{}, err
	}
	info := ScopeInfo{Path: path, SecretStore: secrets.Default(ConfigDir(prefixDir)).Kind()}
	info.Provider, _ = lookup(raw, "llm", "provider").(string)
	info.DefaultModel, _ = lookup(raw, "blitz", "default_model").(string)
	var global map[string]any
	if workspace != "" {
		global, _ = readTOML(filepath.Join(ConfigDir(prefixDir), ".env.toml"))
	}
	store := secrets.Default(ConfigDir(prefixDir))
	for _, p := range KeyedProviders {
		pi := ProviderInfo{Name: p}
		pi.BaseURL, _ = lookup(raw, "llm", p, "base_url").(string)
		pi.Model, _ = lookup(raw, "llm", p, "model").(string)
		pi.Auth, _ = lookup(raw, "llm", p, "auth").(string)
		pi.ProjectID, _ = lookup(raw, "llm", p, "project_id").(string)
		pi.Location, _ = lookup(raw, "llm", p, "location").(string)
		pi.Profile, _ = lookup(raw, "llm", p, "profile").(string)
		key, _ := lookup(raw, "llm", p, "api_key").(string)
		pi.KeySource = keySource(key)
		if name, ok := secrets.ParseRef(key); ok {
			if _, err := store.Get(name); err != nil {
				pi.KeyMissing = true
			}
		}
		if pi.KeySource == KeyNone {
			switch {
			case workspace != "" && keySource(str(lookup(global, "llm", p, "api_key"))) != KeyNone:
				pi.KeySource = KeyInherited
			case envKey(p) != "":
				pi.KeySource = KeyEnvironment
			}
		}
		info.Providers = append(info.Providers, pi)
	}
	return info, nil
}

func str(v any) string { s, _ := v.(string); return s }

func keySource(v string) KeySource {
	switch {
	case v == "":
		return KeyNone
	case strings.HasPrefix(v, secrets.RefPrefix):
		return KeyKeychain
	case strings.HasPrefix(v, "xor:"):
		return KeyObfuscated
	}
	return KeyPlain
}

func envKey(provider string) string {
	for _, v := range keyEnv[provider] {
		if k := os.Getenv(v); k != "" {
			return k
		}
	}
	return ""
}

func scopeDir(prefixDir, workspace string) string {
	if workspace == "" {
		return ConfigDir(prefixDir)
	}
	return WorkspaceSettingsDir(prefixDir, workspace)
}

func readTOML(path string) (map[string]any, error) {
	m := map[string]any{}
	if _, err := toml.DecodeFile(path, &m); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}

func lookup(m map[string]any, path ...string) any {
	var cur any = m
	for _, k := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[k]
	}
	return cur
}

func knownProvider(p string) bool {
	for _, k := range KeyedProviders {
		if k == p {
			return true
		}
	}
	return false
}

// secretName is the stored secret's name for a provider's key in a scope.
func secretName(prefixDir, workspace, provider string) string {
	if workspace == "" {
		return "global/llm." + provider + ".api_key"
	}
	return "workspace/" + filepath.Base(WorkspaceSettingsDir(prefixDir, workspace)) + "/llm." + provider + ".api_key"
}

// SetAPIKey stores key in the OS store and makes the scope's settings file
// refer to it. It returns the file written.
func SetAPIKey(prefixDir, workspace, provider, key string) (string, error) {
	if !knownProvider(provider) {
		return "", fmt.Errorf("%q takes no API key (one of %s)", provider, strings.Join(KeyedProviders, ", "))
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", errors.New("the key is empty")
	}
	name := secretName(prefixDir, workspace, provider)
	undo, err := storeSecret(prefixDir, name, key)
	if err != nil {
		return "", err
	}
	ref := secrets.Ref(name)
	path, err := editConfigFile(scopeDir(prefixDir, workspace),
		func(doc string) string { return setTOMLKey(doc, "llm."+provider, "api_key", strconv.Quote(ref)) },
		func(check map[string]any) error {
			if lookup(check, "llm", provider, "api_key") != ref {
				return fmt.Errorf("could not set [llm.%s] api_key", provider)
			}
			return nil
		})
	if err != nil {
		undo()
	}
	return path, err
}

// storeSecret stores value under name, and returns how to put back what
// was there (or nothing) if the settings file can't then be written.
func storeSecret(prefixDir, name, value string) (undo func(), err error) {
	store := secrets.Default(ConfigDir(prefixDir))
	old, getErr := store.Get(name)
	if err := store.Set(name, value); err != nil {
		return nil, err
	}
	return func() {
		if getErr == nil {
			store.Set(name, old)
		} else if errors.Is(getErr, secrets.ErrNotFound) {
			store.Delete(name)
		}
	}, nil
}

// SecureAPIKey moves a provider's key written in the scope's file (plain
// or modenv's xor:) into the OS store, leaving a reference. It returns the
// file written.
func SecureAPIKey(prefixDir, workspace, provider string) (string, error) {
	if !knownProvider(provider) {
		return "", fmt.Errorf("%q takes no API key", provider)
	}
	raw, err := readTOML(filepath.Join(scopeDir(prefixDir, workspace), ".env.toml"))
	if err != nil {
		return "", err
	}
	v := str(lookup(raw, "llm", provider, "api_key"))
	switch keySource(v) {
	case KeyPlain:
	case KeyObfuscated:
		if v, err = modenv.DecryptSecret(v); err != nil {
			return "", fmt.Errorf("reading the obfuscated [llm.%s] api_key: %w", provider, err)
		}
	default:
		return "", fmt.Errorf("the settings file has no [llm.%s] api_key to move", provider)
	}
	return SetAPIKey(prefixDir, workspace, provider, v)
}

// RemoveAPIKey removes the scope's key for provider: from the file, and from
// the OS store when the file referred to it. A workspace then uses the
// global key again.
func RemoveAPIKey(prefixDir, workspace, provider string) (string, error) {
	if !knownProvider(provider) {
		return "", fmt.Errorf("%q takes no API key", provider)
	}
	dir := scopeDir(prefixDir, workspace)
	raw, err := readTOML(filepath.Join(dir, ".env.toml"))
	if err != nil {
		return "", err
	}
	path, err := editConfigFile(dir,
		func(doc string) string { return removeTOMLKey(doc, "llm."+provider, "api_key") },
		func(check map[string]any) error {
			if lookup(check, "llm", provider, "api_key") != nil {
				return fmt.Errorf("could not remove [llm.%s] api_key", provider)
			}
			return nil
		})
	if err != nil {
		return "", err
	}
	// The file first: a secret left behind is harmless, a reference to
	// one that's gone isn't.
	if name, ok := secrets.ParseRef(str(lookup(raw, "llm", provider, "api_key"))); ok {
		if err := secrets.Default(ConfigDir(prefixDir)).Delete(name); err != nil {
			return path, err
		}
	}
	return path, nil
}

// settableValues are the settings the forms change (SetValue); the rest is
// edited as the file itself.
var settableValues = map[string]bool{
	"llm.provider":             true,
	"blitz.default_model":      true,
	"llm.gemini.model":         true,
	"llm.gemini.auth":          true,
	"llm.gemini.project_id":    true,
	"llm.gemini.location":      true,
	"llm.anthropic.model":      true,
	"llm.anthropic.auth":       true,
	"llm.anthropic.profile":    true,
	"llm.anthropic.project_id": true,
	"llm.anthropic.location":   true,
	"llm.anthropic.base_url":   true,
	"llm.openai.model":         true,
	"llm.openai.base_url":      true,
	"audio.model":              true,
	"audio.voice":              true,
	// What /config --save keeps.
	"blitz.default_agent":   true,
	"blitz.agency_level":    true,
	"blitz.permission_mode": true,
	"ui.style":              true,
	"ui.locale":             true,
	"ui.notify":             true,
	"ui.theme":              true,
	"ui.editor":             true,
	// Workspace search, from its panel in the desktop app.
	"search.embedding_model": true,
	"search.enrich_model":    true,
}

// typedValues are settings the form sets that aren't strings: by key,
// "bool", "int" (0 or more) or "sources" (a comma-separated list of
// search sources).
var typedValues = map[string]string{
	"search.enabled":            "bool",
	"search.enrich":             "bool",
	"search.enrich_daily_limit": "int",
	"search.include_ignored":    "bool",
	"search.sources":            "sources",
}

// SearchSourceNames are the sources workspace search knows.
var SearchSourceNames = []string{"files", "documents", "chats", "notes"}

// typedValue is value as TOML for a key of kind, and as the file then
// reads back, or why it isn't one.
func typedValue(kind, key, value string) (string, any, error) {
	switch kind {
	case "bool":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return "", nil, fmt.Errorf("%s is true or false, not %q", key, value)
		}
		return strconv.FormatBool(b), b, nil
	case "int":
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			return "", nil, fmt.Errorf("%s is a whole number, 0 or more, not %q", key, value)
		}
		return strconv.Itoa(n), int64(n), nil
	case "sources":
		var quoted []string
		var back []any
		for _, s := range strings.Split(value, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if !slices.Contains(SearchSourceNames, s) {
				return "", nil, fmt.Errorf("%q isn't a search source (%s)", s, strings.Join(SearchSourceNames, ", "))
			}
			quoted, back = append(quoted, strconv.Quote(s)), append(back, s)
		}
		if len(quoted) == 0 {
			return "", nil, fmt.Errorf("%s needs at least one source", key)
		}
		return "[" + strings.Join(quoted, ", ") + "]", back, nil
	}
	return "", nil, fmt.Errorf("%s: unknown kind %q", key, kind)
}

// SetValue sets one of the form's settings in a scope (value "" removes it,
// so a workspace follows the global setting again). It returns the file
// written.
func SetValue(prefixDir, workspace, key, value string) (string, error) {
	kind := typedValues[key]
	if !settableValues[key] && kind == "" {
		keys := make([]string, 0, len(settableValues)+len(typedValues))
		for k := range settableValues {
			keys = append(keys, k)
		}
		for k := range typedValues {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return "", fmt.Errorf("%q isn't one of the settings set this way (%s); edit the file for the rest", key, strings.Join(keys, ", "))
	}
	if key == "llm.provider" && value != "" && !ValidProvider(value) {
		return "", fmt.Errorf("unknown provider %q (%s)", value, providerList)
	}
	if provider, ok := strings.CutSuffix(strings.TrimPrefix(key, "llm."), ".auth"); ok {
		if err := (ProviderAuth{Method: value}).check(provider); err != nil {
			return "", err
		}
	}
	literal, want := strconv.Quote(value), any(value)
	if kind != "" && value != "" {
		var err error
		if literal, want, err = typedValue(kind, key, value); err != nil {
			return "", err
		}
	}
	i := strings.LastIndex(key, ".")
	table, name := key[:i], key[i+1:]
	path := strings.Split(key, ".")
	return editConfigFile(scopeDir(prefixDir, workspace),
		func(doc string) string {
			if value == "" {
				return removeTOMLKey(doc, table, name)
			}
			return setTOMLKey(doc, table, name, literal)
		},
		func(check map[string]any) error {
			got := lookup(check, path...)
			if (value == "" && got != nil) || (value != "" && !reflect.DeepEqual(got, want)) {
				return fmt.Errorf("could not set %s", key)
			}
			return nil
		})
}

// ProviderAuth is how a provider signs in: Method "api_key" ("" too), "adc"
// (Gemini, or Claude, on Vertex AI with Application Default Credentials,
// in ProjectID and Location) or "oauth", an account sign-in: Gemini with a
// Google account (on Vertex AI, in ProjectID and Location), Claude with
// the `ant auth login` profile named by Profile (else ant's active one),
// Bedrock with an AWS IAM Identity Center sign-in for Profile, Azure with
// Entra ID ("entra" too). Empty fields are removed, so the environment's
// or the global settings' apply.
type ProviderAuth struct {
	Method    string
	ProjectID string
	Location  string
	Profile   string
}

// authMethods are the sign-in methods each provider takes besides an API
// key (access keys, for Bedrock): Google Cloud's Application Default
// Credentials (Gemini, and Claude on Vertex AI) and an account sign-in,
// OAuth (every provider here; OpenAI and Ollama have none).
var authMethods = map[string][]string{
	"gemini":    {AuthADC, AuthOAuth},
	"anthropic": {AuthOAuth, AuthADC},
	"bedrock":   {AuthOAuth},
	"azure":     {AuthOAuth, AuthEntra},
}

// AuthMethods are the sign-in methods provider takes besides an API key,
// for the forms and the CLI's help.
func AuthMethods(provider string) []string { return slices.Clone(authMethods[provider]) }

func (a ProviderAuth) check(provider string) error {
	if a.Method == "" || a.Method == AuthAPIKey {
		return nil
	}
	if slices.Contains(authMethods[provider], a.Method) {
		if strings.ContainsAny(a.Profile, `/\`) {
			return fmt.Errorf("%q isn't a profile name", a.Profile)
		}
		return nil
	}
	if m := authMethods[provider]; len(m) > 0 {
		return fmt.Errorf("%s signs in with api_key or %s, not %q", provider, strings.Join(m, " or "), a.Method)
	}
	return fmt.Errorf("%s signs in with an API key only, not %q", provider, a.Method)
}

// edits are the settings a ProviderAuth writes for provider ("" removes).
func (a ProviderAuth) edits(provider string) []settingEdit {
	table := "llm." + provider
	method := a.Method
	if method == AuthAPIKey {
		method = ""
	}
	e := []settingEdit{{table, "auth", method}}
	switch {
	case a.Method == AuthADC, a.Method == AuthOAuth && provider == "gemini":
		e = append(e, settingEdit{table, "project_id", a.ProjectID}, settingEdit{table, "location", a.Location})
	case a.Method == AuthOAuth && (provider == "anthropic" || provider == "bedrock"):
		e = append(e, settingEdit{table, "profile", a.Profile})
	}
	return e
}

// settingEdit sets [table] key = value in a settings file ("" removes it).
type settingEdit struct{ table, key, value string }

// editSettings applies edits to a scope's file in one write, checking each.
func editSettings(prefixDir, workspace string, edits []settingEdit) (string, error) {
	return editConfigFile(scopeDir(prefixDir, workspace),
		func(doc string) string {
			for _, e := range edits {
				if e.value == "" {
					doc = removeTOMLKey(doc, e.table, e.key)
				} else {
					doc = setTOMLKey(doc, e.table, e.key, strconv.Quote(e.value))
				}
			}
			return doc
		},
		func(check map[string]any) error {
			for _, e := range edits {
				got := lookup(check, append(strings.Split(e.table, "."), e.key)...)
				if (e.value == "" && got != nil) || (e.value != "" && got != e.value) {
					return fmt.Errorf("could not set [%s] %s", e.table, e.key)
				}
			}
			return nil
		})
}

// SetAuth sets how provider signs in, in a scope. It returns the file
// written.
func SetAuth(prefixDir, workspace, provider string, a ProviderAuth) (string, error) {
	if _, signsIn := authMethods[provider]; !knownProvider(provider) && !signsIn {
		return "", fmt.Errorf("unknown provider %q (%s, bedrock, azure)", provider, strings.Join(KeyedProviders, ", "))
	}
	a.trim()
	if err := a.check(provider); err != nil {
		return "", err
	}
	return editSettings(prefixDir, workspace, a.edits(provider))
}

func (a *ProviderAuth) trim() {
	a.Method, a.ProjectID, a.Location, a.Profile = strings.TrimSpace(a.Method), strings.TrimSpace(a.ProjectID), strings.TrimSpace(a.Location), strings.TrimSpace(a.Profile)
}

// ProviderChoice is what the provider form saves as one change: a scope's
// llm.provider and blitz.default_model ("" removes either, so a workspace
// follows the global setting again), how Provider signs in (Auth; nil
// leaves it as it is) and, unless Key is "", a new API key for Provider.
type ProviderChoice struct {
	Provider string
	Model    string
	Key      string
	Auth     *ProviderAuth
}

// SetProvider saves a ProviderChoice in a scope as one change: everything
// is checked before anything is written, the key goes to the OS store, and
// the file is written once. It returns the file written.
func SetProvider(prefixDir, workspace string, c ProviderChoice) (string, error) {
	c.Model, c.Key = strings.TrimSpace(c.Model), strings.TrimSpace(c.Key)
	if c.Provider != "" && !ValidProvider(c.Provider) {
		return "", fmt.Errorf("unknown provider %q (%s)", c.Provider, providerList)
	}
	if (c.Key != "" || c.Auth != nil) && !knownProvider(c.Provider) {
		return "", fmt.Errorf("choose a provider that takes an API key (%s) for the key or sign-in", strings.Join(KeyedProviders, ", "))
	}
	edits := []settingEdit{{"llm", "provider", c.Provider}, {"blitz", "default_model", c.Model}}
	if c.Auth != nil {
		c.Auth.trim()
		if err := c.Auth.check(c.Provider); err != nil {
			return "", err
		}
		if c.Key != "" && c.Auth.Method != "" && c.Auth.Method != AuthAPIKey {
			return "", fmt.Errorf("an API key is for signing in with api_key, not %s", c.Auth.Method)
		}
		edits = append(edits, c.Auth.edits(c.Provider)...)
	}
	undo := func() {}
	if c.Key != "" {
		name := secretName(prefixDir, workspace, c.Provider)
		var err error
		if undo, err = storeSecret(prefixDir, name, c.Key); err != nil {
			return "", err
		}
		edits = append(edits, settingEdit{"llm." + c.Provider, "api_key", secrets.Ref(name)})
	}
	path, err := editSettings(prefixDir, workspace, edits)
	if err != nil {
		undo()
	}
	return path, err
}

// ReadSettingsFile returns a scope's settings file and its text ("" when it
// doesn't exist yet).
func ReadSettingsFile(prefixDir, workspace string) (string, string, error) {
	dir := scopeDir(prefixDir, workspace)
	if dir == "" {
		return "", "", errors.New("no settings directory")
	}
	path := filepath.Join(dir, ".env.toml")
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", "", err
	}
	return path, string(b), nil
}

// WriteSettingsFile replaces a scope's settings file with text, after
// checking that it's valid TOML that fits Blitz's settings. It returns the
// file written and warnings: keys Blitz doesn't know (likely typos), and
// API keys written in plain text.
func WriteSettingsFile(prefixDir, workspace, text string) (string, []string, error) {
	var warnings []string
	for _, p := range CheckSettings(text) {
		if p.Error {
			return "", nil, fmt.Errorf("not saved: %s", p)
		}
		warnings = append(warnings, p.String())
	}
	path, err := editConfigFile(scopeDir(prefixDir, workspace), func(string) string { return text }, func(map[string]any) error { return nil })
	return path, warnings, err
}

func mustMap(text string) map[string]any {
	m := map[string]any{}
	_, _ = toml.Decode(text, &m)
	return m
}
