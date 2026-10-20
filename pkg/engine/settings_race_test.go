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
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/secrets"
)

// Every way the settings change once a workspace is open (provider keys,
// pins, the reply language, permissions, search, the job policy, MCP)
// runs while the settings are read for a model, a redactor and the pins:
// the race detector finds any of them unguarded.
func TestSettingsChangeWhileRead(t *testing.T) {
	secrets.SetDefault(&secrets.Memory{})
	w := openTest(t)
	ctx := context.Background()
	fresh := func() *config.Config {
		c := *w.settings()
		c.LLM.Gemini.APIKey = "key-" + t.Name()
		return &c
	}
	var wg sync.WaitGroup
	run := func(n int, f func(i int)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range n {
				f(i)
			}
		}()
	}
	const n = 20
	run(n, func(int) { _ = w.ReloadProviders(ctx, fresh()) })
	run(n, func(i int) { w.saveAgentModel("reviewer", map[bool]string{true: "gemini-x", false: ""}[i%2 == 0]) })
	run(n, func(i int) { _, _ = w.SetLocale(ctx, map[bool]string{true: "es", false: "en"}[i%2 == 0]) })
	run(n, func(int) { _ = w.ReloadPermissions(fresh()) })
	run(n, func(int) { w.ReloadWorkerPolicy(fresh()) })
	run(n, func(int) { _ = w.ReloadMCP(ctx, fresh()) })
	run(n, func(int) { _, _ = w.Set(ctx, "agency", "high") })
	run(n, func(int) {
		_ = w.secretRedactor().String("key-" + t.Name())
		_ = w.replyIn().Tag()
		_ = agentModelRefs(w.settings(), w.agents, func(string) {})
		_, _ = w.suggestionsModel(ctx)
	})
	wg.Wait()
	assert.Equal(t, "high", w.settings().Blitz.AgencyLevel)
}
