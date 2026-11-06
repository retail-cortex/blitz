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

package engine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/engine/session"
)

// A conversation's own model (spec_056_conversation_adapters CA-100): set
// with /model, it runs the conversation's turns from the next one, while
// the workspace's model (WS-40) is what a new conversation starts with.

// SessionModel is session id's own model ("" when it runs on the
// workspace's).
func (w *Workspace) SessionModel(id string) string { return w.storage.Model(id) }

// PreviewSessionModel says what moving session id to ref would mean, without
// moving it: what the next turn reads again and costs, whether the
// conversation is compacted first, and what the model can't do.
func (w *Workspace) PreviewSessionModel(ctx context.Context, id, ref string) (api.ModelPreview, error) {
	ref = strings.TrimSpace(ref)
	name := w.engine.ModelName()
	if ref != "" {
		llm, err := w.newModel(ctx, w.settings(), ref)
		if err != nil {
			return api.ModelPreview{}, err
		}
		name = llm.Name()
	}
	cfg := w.settings()
	u := w.engine.Usage(id)
	p := api.ModelPreview{Model: ref, Tokens: u.LastPrompt}
	if price, ok := config.PriceFor(cfg.Pricing, name); ok {
		p.Priced = true
		p.CostUSD = float64(u.LastPrompt) * price.InputPerMTok / 1e6
	}
	c := cfg.Context
	p.Compacts = c.Compaction && c.TokenThreshold > 0 && u.LastPrompt > int64(c.TokenThreshold)
	if m, pinned := w.engine.AgentModel(w.engine.ActiveAgent()); pinned {
		p.ActivePin = m
	}
	return p, nil
}

// SetSessionModel moves session id to ref from its next turn ("" back to
// the workspace's model), compacting it first when it wouldn't fit, and
// returns what the move means (PreviewSessionModel).
func (w *Workspace) SetSessionModel(ctx context.Context, id, ref string) (api.ModelPreview, error) {
	if session.ValidateID(id) != nil {
		return api.ModelPreview{}, fmt.Errorf("%w: %q", api.ErrSessionNotFound, id)
	}
	p, err := w.PreviewSessionModel(ctx, id, ref)
	if err != nil {
		return api.ModelPreview{}, err
	}
	if p.Compacts {
		if _, err := w.engine.Compact(ctx, id, "", 1); err != nil {
			return api.ModelPreview{}, fmt.Errorf("compacting the conversation to fit %s: %w", ref, err)
		}
	}
	if err := w.storage.SetModel(id, p.Model); errors.Is(err, fs.ErrNotExist) {
		return api.ModelPreview{}, fmt.Errorf("%w: %s", api.ErrSessionNotFound, id)
	} else if err != nil {
		return api.ModelPreview{}, err
	}
	return p, nil
}

// conversationModel is the run option for session id's own model on st,
// if it has one.
func (w *Workspace) conversationModel(ctx context.Context, st *session.Storage, id string) ([]runtime.ExecOption, error) {
	ref := st.Model(id)
	if ref == "" {
		return nil, nil
	}
	llm, err := w.newModel(ctx, w.settings(), ref)
	if err != nil {
		return nil, fmt.Errorf("this conversation's model %s: %s", ref, ModelErrorSummary(err, w.settings()))
	}
	return []runtime.ExecOption{runtime.WithConversationModel(llm)}, nil
}
