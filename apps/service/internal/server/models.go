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

package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// The models forms suggest and check names against: each configured
// provider's list, from its API, kept a few minutes per scope so opening a
// form doesn't call every provider.

const (
	// modelsFresh is how long a listing is kept; modelsRetry, one in
	// which a provider failed (a key may be added meanwhile).
	modelsFresh = 10 * time.Minute
	modelsRetry = time.Minute
)

// modelLists caches listings by scope ("" for the user's settings).
type modelLists struct {
	mu   sync.Mutex
	byID map[string]modelList
}

type modelList struct {
	res     *pb.ListModelsResponse
	expires time.Time
}

// listModels asks the providers cfg configures for their models; a
// variable so tests needn't reach them.
var listModels = runtime.ListModels

func (h configService) ListModels(ctx context.Context, r req[pb.ListModelsRequest]) (*connect.Response[pb.ListModelsResponse], error) {
	dir, err := scope(r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	key := dir + "\x00" + strings.Join(r.Msg.Providers, ",")
	c := &h.s.models
	c.mu.Lock()
	if l, kept := c.byID[key]; kept && time.Now().Before(l.expires) {
		c.mu.Unlock()
		return ok(l.res)
	}
	c.mu.Unlock()

	var cfg *config.Config
	if dir == "" {
		cfg, err = config.Load(h.s.configDir)
	} else {
		cfg, err = config.LoadWorkspace(h.s.configDir, dir)
	}
	if err != nil {
		return nil, invalid(err)
	}
	res := &pb.ListModelsResponse{DefaultProvider: strings.ToLower(cfg.LLM.Provider)}
	fresh := modelsFresh
	for _, pm := range listModels(ctx, cfg, r.Msg.Providers...) {
		p := &pb.ProviderModels{Provider: pm.Provider, Note: pm.Note}
		if pm.Err != nil {
			p.Error = pm.Err.Error()
			fresh = modelsRetry
		}
		for _, m := range pm.Models {
			p.Ids = append(p.Ids, m.ID)
			lm := &pb.ListedModel{Id: m.ID}
			if m.Price != nil {
				lm.HasPrice, lm.InputPerMtok, lm.OutputPerMtok = true, m.Price.InputPerMTok, m.Price.OutputPerMTok
			}
			p.Models = append(p.Models, lm)
		}
		res.Providers = append(res.Providers, p)
	}
	c.mu.Lock()
	if c.byID == nil {
		c.byID = map[string]modelList{}
	}
	c.byID[key] = modelList{res: res, expires: time.Now().Add(fresh)}
	c.mu.Unlock()
	return ok(res)
}

// forget drops the listings, as when keys or providers change.
func (c *modelLists) forget() {
	c.mu.Lock()
	c.byID = nil
	c.mu.Unlock()
}

// CheckApiKey lists the provider's models with a key that isn't saved:
// the scope's settings with the key in place of the provider's own
// (and its key command and sign-in set aside), so the answer is about
// the key alone.
func (h configService) CheckApiKey(ctx context.Context, r req[pb.CheckApiKeyRequest]) (*connect.Response[pb.CheckApiKeyResponse], error) {
	dir, err := scope(r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	key := strings.TrimSpace(r.Msg.Key)
	if key == "" {
		return nil, invalid(errors.New("no key to check"))
	}
	var cfg *config.Config
	if dir == "" {
		cfg, err = config.Load(h.s.configDir)
	} else {
		cfg, err = config.LoadWorkspace(h.s.configDir, dir)
	}
	if err != nil {
		return nil, invalid(err)
	}
	switch r.Msg.Provider {
	case "gemini":
		g := &cfg.LLM.Gemini
		g.APIKey, g.APIKeyCommand, g.Auth = key, "", ""
	case "anthropic":
		a := &cfg.LLM.Anthropic
		a.APIKey, a.APIKeyCommand, a.Auth = key, "", ""
	case "openai":
		o := &cfg.LLM.OpenAI
		o.APIKey, o.APIKeyCommand = key, ""
	default:
		return nil, invalid(fmt.Errorf("provider %q takes no API key to check (gemini, anthropic or openai)", r.Msg.Provider))
	}
	pm := listModels(ctx, cfg, r.Msg.Provider)[0]
	res := &pb.CheckApiKeyResponse{Works: pm.Err == nil}
	if pm.Err != nil {
		res.Error = pm.Err.Error()
	}
	for _, m := range pm.Models {
		res.Models = append(res.Models, m.ID)
	}
	return ok(res)
}
