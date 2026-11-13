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
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/retail-cortex/blitz/pkg/config"
)

// The api provider's models (spec_015_models MDL-114): what the gateway
// lists at models_path, asked as its model requests are signed, or what
// another server lists at a URL, asked with models_headers (MDL-115).

// maxModelList is how much of the gateway's list is read.
const maxModelList = 4 << 20

// errModelList is a list the gateway answered with an error.
var errModelList = errors.New("the gateway's model list")

// listAPIModels is the models the gateway lists, else (models_path =
// "none", or when it can't be read, with the error) the configured ones.
func listAPIModels(ctx context.Context, c config.APIConfig, pol retryPolicy) ([]string, error) {
	if _, ok := c.ModelsList(); !ok {
		return configuredAPIModels(c), nil
	}
	ids, err := fetchAPIModels(ctx, c, pol)
	if err != nil {
		return configuredAPIModels(c), err
	}
	return ids, nil
}

// configuredAPIModels are [llm.api] model and models, then the models
// [llm.api.apis] names (the desktop form's rows), sorted.
func configuredAPIModels(c config.APIConfig) []string {
	ids := nonEmpty(c.Model)
	named := make([]string, 0, len(c.APIs))
	for m := range c.APIs {
		if m != "*" {
			named = append(named, m)
		}
	}
	slices.Sort(named)
	for _, m := range slices.Concat(c.Models, named) {
		if !slices.Contains(ids, m) {
			ids = append(ids, m)
		}
	}
	return ids
}

// fetchAPIModels GETs the list (config's ModelsList), signed with its
// headers and the token (MDL-101, MDL-104, MDL-115), and reads it.
func fetchAPIModels(ctx context.Context, c config.APIConfig, pol retryPolicy) ([]string, error) {
	if err := c.Check(); err != nil {
		return nil, err
	}
	list, _ := c.ModelsList()
	p := &apiProvider{cfg: list, format: formatResponses}
	if list.UsesToken() {
		p.tokens = gatewayTokens(list, p.fetchToken) // the gateway's, by its script
	}
	where, err := p.baseURL()
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, where, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	res, err := p.middleware(req, pol.httpClient().Do)
	if err != nil {
		return nil, apiError(err, nil, false)
	}
	defer discardResponse(res)
	body, err := io.ReadAll(io.LimitReader(res.Body, maxModelList))
	if err != nil {
		return nil, apiError(err, nil, false)
	}
	if res.StatusCode/100 != 2 {
		return nil, apiError(errModelList, &errorReply{res.StatusCode, string(body[:min(len(body), maxErrorBody)])}, false)
	}
	return parseModelList(body)
}

// listedModel is one model in a list: OpenAI's and Anthropic's id,
// Gemini's name (models/…), or another server's model.
type listedModel struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Model   string   `json:"model"`
	Methods []string `json:"supportedGenerationMethods"`
}

// UnmarshalJSON reads a model given as its name alone, too.
func (m *listedModel) UnmarshalJSON(b []byte) error {
	if json.Unmarshal(b, &m.ID) == nil {
		return nil
	}
	type plain listedModel
	return json.Unmarshal(b, (*plain)(m))
}

// parseModelList reads a list of models in the shapes servers give it:
// {"data": [...]} (OpenAI's, Anthropic's and their compatible servers'),
// {"models": [...]} (Gemini's, Ollama's), or the list itself. A Gemini
// model that can't generateContent is left out.
func parseModelList(body []byte) ([]string, error) {
	var doc struct {
		Data   []listedModel `json:"data"`
		Models []listedModel `json:"models"`
	}
	var list []listedModel
	if err := json.Unmarshal(body, &doc); err == nil {
		list = append(doc.Data, doc.Models...)
	} else if err := json.Unmarshal(body, &list); err != nil {
		return nil, errors.New("[llm.api] the gateway's model list isn't JSON: set models_path, or none")
	}
	var ids []string
	for _, m := range list {
		if len(m.Methods) > 0 && !slices.Contains(m.Methods, "generateContent") {
			continue
		}
		id := strings.TrimPrefix(strings.TrimPrefix(cmp.Or(m.ID, m.Model, m.Name), "publishers/google/"), "models/")
		if id != "" && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, errors.New("[llm.api] the gateway lists no models: set models_path, or none")
	}
	return ids, nil
}
