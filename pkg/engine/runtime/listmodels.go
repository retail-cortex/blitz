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
	"cmp"
	"context"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/enterprise"
	"google.golang.org/genai"
)

// ProviderModels are the models a configured provider offers
// (spec_027_parity PAR-MOD-03), or why they couldn't be listed.
type ProviderModels struct {
	Provider string
	Models   []ListedModel
	Err      error
	// Note says what the list is when it isn't the provider's own.
	Note string
}

// ListedModel is one of them, with the price Blitz knows for it.
type ListedModel struct {
	ID    string
	Price *config.ModelPrice
	// DisplayName and Default are the organization's, for its models.
	DisplayName string
	Default     bool
	// Caps are what the model can do: what the organization says of its
	// model (EN-21), else Blitz's facts for the name (spec_061_models
	// MOD-01); nil when the organization says nothing.
	Caps *enterprise.Capabilities
}

// listTimeout bounds each provider's listing.
const listTimeout = 20 * time.Second

// ConfiguredProviders are the providers cfg has credentials (or, for
// Ollama, a choice) for. Connected to an organization, its service first,
// alone unless its policy allows the person's own beside it
// (spec_053_enterprise EN-20, EN-35).
func ConfiguredProviders(cfg *config.Config) []string {
	out := setUpProviders(cfg)
	if active := cfg.LLM.Providers; len(active) > 0 { // spec_061_models MOD-40
		out = slices.DeleteFunc(out, func(p string) bool { return p != "enterprise" && !slices.Contains(active, p) })
	}
	return out
}

// setUpProviders are the providers cfg has credentials (or settings) for,
// active or not.
func setUpProviders(cfg *config.Config) []string {
	var out []string
	if cfg.Enterprise.URL != "" {
		out = []string{"enterprise"}
		if !cfg.Org.Policy.Models.OwnProviders {
			return out
		}
	}
	if cfg.LLM.Gemini.APIKey != "" || cfg.LLM.Gemini.APIKeyCommand != "" || cfg.LLM.Gemini.UsesADC() {
		out = append(out, "gemini")
	}
	a := cfg.LLM.Anthropic
	if a.APIKey != "" || a.APIKeyCommand != "" || a.UsesOAuth() || a.UsesADC() {
		out = append(out, "anthropic")
	}
	if cfg.LLM.OpenAI.APIKey != "" || cfg.LLM.OpenAI.APIKeyCommand != "" {
		out = append(out, "openai")
	}
	if strings.EqualFold(cfg.LLM.Provider, "ollama") || cfg.LLM.Ollama.BaseURL != "" {
		out = append(out, "ollama")
	}
	if cfg.LLM.Bedrock.Model != "" || strings.EqualFold(cfg.LLM.Provider, "bedrock") {
		out = append(out, "bedrock")
	}
	if cfg.LLM.Azure.Resource != "" || cfg.LLM.Azure.BaseURL != "" || cfg.LLM.Azure.AnthropicBaseURL != "" {
		out = append(out, "azure")
	}
	if cfg.LLM.API.BaseURL != "" {
		out = append(out, "api")
	}
	return out
}

// ListModels asks each provider (all configured ones when none are named)
// for its models, in parallel.
func ListModels(ctx context.Context, cfg *config.Config, providers ...string) []ProviderModels {
	if len(providers) == 0 {
		providers = ConfiguredProviders(cfg)
		// Ollama on this machine, when it answers (MOD-03).
		active := cfg.LLM.Providers
		if ownProviders(cfg) && !slices.Contains(providers, "ollama") && (len(active) == 0 || slices.Contains(active, "ollama")) && ollamaAnswers(ctx) {
			providers = append(providers, "ollama")
		}
	}
	prices := NewUsageTracker(cfg.Pricing)
	out := make([]ProviderModels, len(providers))
	done := make(chan struct{}, len(providers))
	for i, p := range providers {
		go func() {
			defer func() { done <- struct{}{} }()
			lctx, cancel := context.WithTimeout(ctx, listTimeout)
			defer cancel()
			if p == "enterprise" {
				out[i] = listEnterprise(lctx, cfg)
				return
			}
			ids, err := listProvider(lctx, cfg, p)
			sort.Strings(ids)
			pm := ProviderModels{Provider: p, Err: err}
			switch p {
			case "bedrock", "azure":
				pm.Note = "the configured model: " + p + "'s console lists the others"
			case "api":
				pm.Note = "the configured model"
			}
			for _, id := range ids {
				m := ListedModel{ID: id}
				if price, ok := prices.price(id); ok {
					m.Price = &price
				}
				info := Info("", p, id)
				m.Caps = &enterprise.Capabilities{Tools: info.Tools != ToolsNone, Images: info.Vision, Thinking: info.Thinking != ThinkingNone, Context: info.ContextWindow}
				pm.Models = append(pm.Models, m)
			}
			out[i] = pm
		}()
	}
	for range providers {
		<-done
	}
	return out
}

func listProvider(ctx context.Context, cfg *config.Config, provider string) ([]string, error) {
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
		var ids []string
		for m, err := range client.Models.All(ctx) {
			if err != nil {
				return ids, err
			}
			if len(m.SupportedActions) == 0 || contains(m.SupportedActions, "generateContent") {
				ids = append(ids, strings.TrimPrefix(strings.TrimPrefix(m.Name, "publishers/google/"), "models/"))
			}
		}
		return ids, nil
	case "anthropic":
		m, err := newAnthropicModel(ctx, cfg.LLM.Anthropic, cmp.Or(cfg.LLM.Anthropic.Model, "claude"), pol.anthropicOptions()...)
		if err != nil {
			return nil, err
		}
		var ids []string
		pager := m.client.Models.ListAutoPaging(ctx, anthropic.ModelListParams{})
		for pager.Next() {
			ids = append(ids, pager.Current().ID)
		}
		return ids, pager.Err()
	case "bedrock":
		return nonEmpty(cfg.LLM.Bedrock.Model), nil
	case "azure":
		return nonEmpty(cfg.LLM.Azure.Model), nil
	case "api":
		return nonEmpty(cfg.LLM.API.Model), nil
	case "openai", "ollama":
		key, base, opts, err := openAIKey(ctx, cfg.LLM, provider, pol)
		if err != nil {
			return nil, err
		}
		opts = append(opts, option.WithAPIKey(key))
		if base != "" {
			opts = append(opts, option.WithBaseURL(base))
		}
		client := openai.NewClient(opts...)
		var ids []string
		pager := client.Models.ListAutoPaging(ctx)
		for pager.Next() {
			ids = append(ids, pager.Current().ID)
		}
		return ids, pager.Err()
	}
	return nil, errUnknownProvider(provider)
}

func nonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

type errUnknownProvider string

func (e errUnknownProvider) Error() string {
	return "unknown provider " + strconv.Quote(string(e)) + " (" + config.ProviderList() + ")"
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// listEnterprise is the organization's models the person may use, at its
// prices (EN-21).
func listEnterprise(ctx context.Context, cfg *config.Config) ProviderModels {
	pm := ProviderModels{Provider: "enterprise"}
	c, err := enterpriseClient(cfg)
	if err != nil {
		pm.Err = err
		return pm
	}
	list, err := c.Models(ctx)
	if err != nil {
		pm.Err = err
		return pm
	}
	for _, m := range list {
		lm := ListedModel{ID: m.ID, DisplayName: m.DisplayName, Default: m.Default}
		if c, ok := m.Caps(); ok {
			lm.Caps = &c
		}
		if m.Price != nil {
			lm.Price = &config.ModelPrice{InputPerMTok: m.Price.InputPerMTok, OutputPerMTok: m.Price.OutputPerMTok}
		}
		pm.Models = append(pm.Models, lm)
	}
	sort.Slice(pm.Models, func(i, j int) bool { return pm.Models[i].ID < pm.Models[j].ID })
	return pm
}

// ownProviders reports whether the person's own providers may be used:
// no organization, or one whose policy allows them (EN-35).
func ownProviders(cfg *config.Config) bool {
	return cfg.Enterprise.URL == "" || cfg.Org.Policy.Models.OwnProviders
}

// ollamaAddress is where Ollama answers on this machine by default.
const ollamaAddress = "http://localhost:11434/api/tags"

// ollamaAnswers reports whether Ollama answers on this machine within a
// moment (spec_061_models MOD-03); a variable so tests decide. In a test
// binary it never does: an Ollama running where tests run isn't theirs.
var ollamaAnswers = func(ctx context.Context) bool {
	if testing.Testing() {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ollamaAddress, nil)
	if err != nil {
		return false
	}
	req.Close = true // one question: no connection kept open
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	_ = res.Body.Close()
	return res.StatusCode == http.StatusOK
}

// OllamaAnswers reports whether Ollama answers on this machine (MOD-03;
// doctor's check, spec_060 DRV-81).
func OllamaAnswers(ctx context.Context) bool { return ollamaAnswers(ctx) }

// notChat matches the models that can't hold a conversation: embeddings,
// image, video, music and speech models, and retired families providers
// still list (spec_061_models MOD-02).
var notChat = regexp.MustCompile(`(?i)embed|imagen|(^|[-_/])image([-_]|$)|gpt-image|dall-e|veo|lyria|(^|[-_])tts([-_]|$)|whisper|transcribe|moderation|(^|[-_/])aqa($|[-_])|learnlm|native-audio|realtime`)

// ChatModel reports whether id is a model that holds a conversation; a
// name Blitz doesn't know is kept, since it may be new.
func ChatModel(id string) bool { return !notChat.MatchString(id) }

// ChatModels is pms with only their chat models (ChatModel), each
// provider's newest family first, then by name (spec_061_models MOD-01,
// MOD-02).
func ChatModels(pms []ProviderModels) []ProviderModels {
	out := make([]ProviderModels, len(pms))
	for i, pm := range pms {
		pm.Models = slices.DeleteFunc(slices.Clone(pm.Models), func(m ListedModel) bool { return !ChatModel(m.ID) })
		slices.SortStableFunc(pm.Models, func(a, b ListedModel) int {
			if c := compareVersions(modelVersion(b.ID), modelVersion(a.ID)); c != 0 {
				return c
			}
			return strings.Compare(a.ID, b.ID)
		})
		out[i] = pm
	}
	return out
}

// versionIn finds a model name's version: the first run of numbers
// joined by dots or hyphens (gemini-3.8-flash: 3.8; claude-opus-5-5: 5-5).
var versionIn = regexp.MustCompile(`\d+(?:[.-]\d+)*`)

// modelVersion is the numbers of id's version (none when it has none).
func modelVersion(id string) []int {
	var v []int
	for _, part := range regexp.MustCompile(`[.-]`).Split(versionIn.FindString(id), -1) {
		n, err := strconv.Atoi(part)
		if err != nil {
			break
		}
		v = append(v, n)
	}
	return v
}

// compareVersions orders versions number by number; more numbers after
// an equal start is newer (3.8 after 3).
func compareVersions(a, b []int) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if c := cmp.Compare(a[i], b[i]); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(a), len(b))
}

// ownProviderNames are the providers a person sets up themselves, in the
// order Settings lists them.
var ownProviderNames = []string{"gemini", "anthropic", "openai", "ollama"}

// NotSetUp are the providers the person could set up and hasn't, given
// those listed (spec_061_models MOD-06); none under an organization that
// doesn't allow their own.
func NotSetUp(cfg *config.Config, listed []ProviderModels) []string {
	if !ownProviders(cfg) {
		return nil
	}
	var out []string
	for _, p := range ownProviderNames {
		if !slices.ContainsFunc(listed, func(pm ProviderModels) bool { return pm.Provider == p }) {
			out = append(out, p)
		}
	}
	return out
}

// latestProviders are the providers whose menus list their latest models
// when the person chose none (MOD-43); the others list all of theirs.
var latestProviders = map[string]bool{"gemini": true, "anthropic": true, "openai": true}

// snapshotIn matches what makes a name a preview, an experiment or a dated
// snapshot of its family's model.
var snapshotIn = regexp.MustCompile(`-(preview|exp(erimental)?)(-[0-9]+(-[0-9]+)*)?|-latest$|-[0-9]{4}-[0-9]{2}-[0-9]{2}$|-[0-9]{8}$|-[0-9]{4}$`)

// modelFamily is id's family (its name with its version as #) and whether
// it's a preview or snapshot; the version is modelVersion's.
func modelFamily(id string) (family string, snapshot bool) {
	base := snapshotIn.ReplaceAllString(id, "")
	snapshot = base != id
	if loc := versionIn.FindStringIndex(base); loc != nil {
		base = base[:loc[0]] + "#" + base[loc[1]:]
	}
	return base, snapshot
}

// LatestModels are a provider's latest models (spec_061_models MOD-43): of
// each family its newest version, a stable one over a preview or snapshot
// of the same version, of the families whose newest is within one major
// version of the provider's newest; in the order given.
func LatestModels(models []ListedModel) []ListedModel {
	type pick struct {
		at       int
		version  []int
		snapshot bool
	}
	best := map[string]pick{}
	top := 0
	for i, m := range models {
		fam, snap := modelFamily(m.ID)
		v := modelVersion(snapshotIn.ReplaceAllString(m.ID, "")) // a date isn't a version
		if len(v) > 0 {
			top = max(top, v[0])
		}
		cur, seen := best[fam]
		c := compareVersions(v, cur.version)
		if !seen || c > 0 || c == 0 && cur.snapshot && !snap {
			best[fam] = pick{i, v, snap}
		}
	}
	keep := map[int]bool{}
	for _, p := range best {
		if len(p.version) == 0 || p.version[0] >= top-1 {
			keep[p.at] = true
		}
	}
	var out []ListedModel
	for i, m := range models {
		if keep[i] {
			out = append(out, m)
		}
	}
	return out
}

// MenuModels are what the model menus list for each provider of pms
// (MOD-44): the person's chosen models (MOD-42), else its latest (MOD-43)
// for the major providers and all of them for the rest; and always the
// model in use, current ("provider/name" or a name of cfg's provider).
func MenuModels(cfg *config.Config, pms []ProviderModels, current string) []ProviderModels {
	curProvider, curName := ParseModelRef(current, cfg.LLM.Provider)
	out := make([]ProviderModels, len(pms))
	for i, pm := range pms {
		chosen := chosenModels(cfg, pm.Provider)
		switch {
		case len(chosen) > 0:
			var kept []ListedModel
			for _, id := range chosen {
				if j := slices.IndexFunc(pm.Models, func(m ListedModel) bool { return m.ID == id }); j >= 0 {
					kept = append(kept, pm.Models[j])
				} else {
					kept = append(kept, ListedModel{ID: id}) // chosen, though the provider doesn't list it now
				}
			}
			pm.Models = kept
		case latestProviders[pm.Provider]:
			all := pm.Models
			pm.Models = LatestModels(all)
			if curProvider == pm.Provider && !slices.ContainsFunc(pm.Models, func(m ListedModel) bool { return m.ID == curName }) {
				if j := slices.IndexFunc(all, func(m ListedModel) bool { return m.ID == curName }); j >= 0 {
					pm.Models = append(pm.Models, all[j])
				}
			}
		}
		out[i] = pm
	}
	return out
}

// chosenModels are the models the person chose for provider's menus.
func chosenModels(cfg *config.Config, provider string) []string {
	switch provider {
	case "gemini":
		return cfg.LLM.Gemini.Models
	case "anthropic":
		return cfg.LLM.Anthropic.Models
	case "openai":
		return cfg.LLM.OpenAI.Models
	case "ollama":
		return cfg.LLM.Ollama.Models
	}
	return nil
}
