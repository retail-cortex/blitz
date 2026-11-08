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
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// With an Ollama model and no reviewer of its own, auto mode is ask, with
// a note saying why; a cloud model gives auto back (spec_005 APR-14c).
func TestLocalModelHoldsAutoMode(t *testing.T) {
	ctx := context.Background()
	w, _ := openTestWith(t, func(c *config.Config) {
		c.Blitz.PermissionMode = "auto"
		c.LLM.Provider = "ollama"
		c.LLM.Ollama.Model = "qwen2.5-coder:7b"
	})
	s := w.Settings()
	assert.Equal(t, "default", s.PermissionMode, "a local model doesn't review its own actions")
	assert.Contains(t, s.ModeNote, "Ollama")

	mode, err := w.SetPermissionMode("auto")
	require.NoError(t, err)
	assert.Equal(t, "default", mode, "auto chosen again is still ask")

	_, err = w.SetModel(ctx, "gemini/gemini-3.8-flash")
	require.NoError(t, err)
	assert.Equal(t, "auto", w.Settings().PermissionMode, "a cloud model gives auto back")
	assert.Empty(t, w.Settings().ModeNote)

	_, err = w.SetModel(ctx, "ollama/qwen3:1.7b")
	require.NoError(t, err)
	assert.Equal(t, "default", w.Settings().PermissionMode, "switching to a local model holds it again")
	assert.NotEmpty(t, w.Settings().ModeNote)

	// Ask chosen while the model is local is the person's own choice: a
	// cloud model doesn't turn it into auto.
	mode, err = w.SetPermissionMode("default")
	require.NoError(t, err)
	assert.Equal(t, "default", mode)
	_, err = w.SetModel(ctx, "gemini/gemini-3.8-flash")
	require.NoError(t, err)
	assert.Equal(t, "default", w.Settings().PermissionMode)

	// The other modes aren't touched.
	_, err = w.SetModel(ctx, "ollama/qwen3:1.7b")
	require.NoError(t, err)
	mode, err = w.SetPermissionMode("accept-edits")
	require.NoError(t, err)
	assert.Equal(t, "accept-edits", mode)
}

// A reviewer model of its own keeps auto mode with a local model.
func TestLocalModelWithItsOwnReviewer(t *testing.T) {
	w, _ := openTestWith(t, func(c *config.Config) {
		c.Blitz.PermissionMode = "auto"
		c.LLM.Provider = "ollama"
		c.LLM.Ollama.Model = "qwen2.5-coder:7b"
		c.Permissions.Auto.Model = "gemini/gemini-3.8-flash"
	})
	s := w.Settings()
	assert.Equal(t, "auto", s.PermissionMode)
	assert.Empty(t, s.ModeNote)
}

func TestLocalReview(t *testing.T) {
	cases := []struct {
		name        string
		provider    string
		model       string
		ownReviewer bool
		want        bool
	}{
		{name: "ollama", provider: "ollama", model: "qwen2.5-coder:7b", want: true},
		{name: "an ollama model named", provider: "gemini", model: "ollama/llama3.2", want: true},
		{name: "a cloud model", provider: "gemini", model: "gemini-3.8-flash"},
		{name: "a cloud model named on ollama", provider: "ollama", model: "gemini/gemini-3.8-flash"},
		{name: "a reviewer of its own", provider: "ollama", model: "qwen2.5-coder:7b", ownReviewer: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.LLM.Provider = c.provider
			cfg.Blitz.DefaultModel = c.model
			assert.Equal(t, c.want, localReview(cfg, c.ownReviewer))
		})
	}
}
