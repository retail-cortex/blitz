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
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"
)

// ollamaShowTimeout bounds asking Ollama about a model: it answers at
// once when it's running, and nothing waits on it.
var ollamaShowTimeout = 3 * time.Second

// listOllama asks Ollama what it knows of model (POST /api/show): whether
// it calls tools and reads images, and its context length (spec_056 CA-11,
// CA-64). baseURL is its OpenAI endpoint (…/v1). Silence teaches nothing.
func listOllama(baseURL, model string) {
	ctx, cancel := context.WithTimeout(context.Background(), ollamaShowTimeout)
	defer cancel()
	f, ok := showOllama(ctx, baseURL, model)
	if ok {
		setListed("ollama", model, f)
	}
}

func showOllama(ctx context.Context, baseURL, model string) (listedFacts, bool) {
	base := strings.TrimSuffix(strings.TrimSuffix(baseURL, "/"), "/v1")
	body, _ := json.Marshal(map[string]string{"model": model})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/show", bytes.NewReader(body))
	if err != nil {
		return listedFacts{}, false
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return listedFacts{}, false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return listedFacts{}, false
	}
	var show struct {
		Capabilities []string       `json:"capabilities"`
		ModelInfo    map[string]any `json:"model_info"`
	}
	if json.NewDecoder(res.Body).Decode(&show) != nil {
		return listedFacts{}, false
	}
	var f listedFacts
	if len(show.Capabilities) > 0 { // older Ollama says nothing of them
		tools, vision := slices.Contains(show.Capabilities, "tools"), slices.Contains(show.Capabilities, "vision")
		f.Tools, f.Vision = &tools, &vision
	}
	for k, v := range show.ModelInfo {
		if n, isNum := v.(float64); isNum && strings.HasSuffix(k, ".context_length") && n > 0 {
			f.ContextWindow = int(n)
		}
	}
	return f, f.Tools != nil || f.ContextWindow > 0
}
