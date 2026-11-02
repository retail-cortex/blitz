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
	"sort"
	"strings"
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
	// Caps are what the organization says its model can do (EN-21); nil
	// when it says nothing, and for every other provider's.
	Caps *enterprise.Capabilities
}

// listTimeout bounds each provider's listing.
const listTimeout = 20 * time.Second

// ConfiguredProviders are the providers cfg has credentials (or, for
// Ollama, a choice) for. Connected to an organization, its service first,
// alone unless its policy allows the person's own beside it
// (spec_053_enterprise EN-20, EN-35).
func ConfiguredProviders(cfg *config.Config) []string {
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
	if strings.EqualFold(cfg.LLM.Provider, "ollama") {
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
	return "unknown provider " + string(e) + " (gemini, anthropic, openai, ollama, bedrock, azure or api)"
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
