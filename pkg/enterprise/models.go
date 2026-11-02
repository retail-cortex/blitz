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

package enterprise

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/retail-cortex/blitz/pkg/secrets"
)

// The APIs a model speaks, as the service names them.
const (
	APIMessages  = "messages"  // Anthropic's Messages API
	APIResponses = "responses" // OpenAI's Responses API
)

// Price is a model's, in dollars per million tokens.
type Price struct {
	InputPerMTok  float64 `json:"input_per_mtok"`
	OutputPerMTok float64 `json:"output_per_mtok"`
}

// Model is one of the organization's models the person may use (EN-21).
type Model struct {
	ID              string         `json:"id"`
	DisplayName     string         `json:"display_name"`
	Vendor          string         `json:"vendor"`
	API             string         `json:"api"`
	Capabilities    map[string]any `json:"capabilities"`
	Price           *Price         `json:"price"`
	Default         bool           `json:"default"`
	MaxOutputTokens int            `json:"max_output_tokens"`
}

// Capabilities are what a model can do, as the organization says
// (EN-21): whether it uses tools, reads images and thinks before
// answering, and its context window in tokens (0 when unknown).
type Capabilities struct {
	Tools, Images, Thinking bool
	Context                 int
}

// Caps are m's capabilities; ok is false when the service says none.
func (m Model) Caps() (c Capabilities, ok bool) {
	if len(m.Capabilities) == 0 {
		return c, false
	}
	is := func(k string) bool { v, _ := m.Capabilities[k].(bool); return v }
	c = Capabilities{Tools: is("tools"), Images: is("images"), Thinking: is("thinking")}
	if n, isNum := m.Capabilities["context"].(float64); isNum && n > 0 {
		c.Context = int(n)
	}
	return c, true
}

// models is the last list and its ETag, asked again with If-None-Match.
type models struct {
	mu   sync.Mutex
	etag string
	list []Model
}

// Models are the organization's models the person may use: the service
// filters them by role. The list is kept and asked for again with its
// ETag, so an unchanged one costs the service nothing.
func (c *Client) Models(ctx context.Context) ([]Model, error) {
	token, err := c.AccessToken(ctx)
	if err != nil {
		return nil, err
	}
	c.models.mu.Lock()
	defer c.models.mu.Unlock()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url+"/api/v1/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if c.models.etag != "" {
		req.Header.Set("If-None-Match", c.models.etag)
	}
	res, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reaching the organization's service: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotModified && c.models.etag != "" {
		return c.models.list, nil
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		var r struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &r)
		if res.StatusCode == http.StatusUnauthorized {
			c.Invalidate()
		}
		return nil, &Refusal{Status: res.StatusCode, Code: r.Code, Message: r.Message}
	}
	var out struct {
		Data []Model `json:"data"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("the organization's models: %w", err)
	}
	c.models.etag, c.models.list = res.Header.Get("ETag"), out.Data
	return out.Data, nil
}

// Find is the model id in list, else the default one, else the first:
// a model the settings name that the organization doesn't offer gives
// way to one it does. ok is false only for an empty list.
func Find(list []Model, id string) (m Model, ok bool) {
	for _, m := range list {
		if m.ID == id {
			return m, true
		}
	}
	for _, m := range list {
		if m.Default {
			return m, true
		}
	}
	if len(list) > 0 {
		return list[0], true
	}
	return Model{}, false
}

// shared holds the one Client for each service in this process, so its
// sign-in renews in one place.
var shared = struct {
	sync.Mutex
	clients map[string]*Client
}{clients: map[string]*Client{}}

// Register makes c the process's Client for its service: blitzd's,
// with the store and the name it signs in with.
func Register(c *Client) {
	shared.Lock()
	defer shared.Unlock()
	shared.clients[c.url] = c
}

// Shared is the process's Client for the service at u (normalized),
// made with store and o when none is registered yet.
func Shared(u string, store func() secrets.Store, o Options) *Client {
	shared.Lock()
	defer shared.Unlock()
	if c := shared.clients[u]; c != nil {
		return c
	}
	c := New(u, store(), o)
	shared.clients[u] = c
	return c
}
