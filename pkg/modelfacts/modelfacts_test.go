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

package modelfacts

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLearnAndFacts(t *testing.T) {
	path := Path(t.TempDir())
	s := Open(path)
	now := time.Now()
	assert.Empty(t, s.Facts("ollama", "llama3", now), "no file, no facts")

	fresh, err := s.Learn("Ollama", "Llama3", Fact{Kind: NoTools, Why: "does not support tools", At: now})
	require.NoError(t, err)
	assert.True(t, fresh)
	fresh, err = s.Learn("ollama", "llama3", Fact{Kind: NoTools, Why: "again", At: now})
	require.NoError(t, err)
	assert.False(t, fresh, "the same fact again is told once")
	_, err = s.Learn("ollama", "llama3", Fact{Kind: NoParam, Value: "seed", Why: "x", At: now})
	require.NoError(t, err)
	_, err = s.Learn("ollama", "llama3", Fact{Kind: NoParam, Value: "top_p", Why: "x", At: now})
	require.NoError(t, err)
	fresh, err = s.Learn("ollama", "llama3", Fact{Kind: ContextWindow, Value: "8192", Why: "x", At: now})
	require.NoError(t, err)
	assert.True(t, fresh)
	fresh, err = s.Learn("ollama", "llama3", Fact{Kind: ContextWindow, Value: "4096", Why: "x", At: now})
	require.NoError(t, err)
	assert.True(t, fresh, "a new limit is news")

	facts := Open(path).Facts("ollama", "llama3", now) // another process reads the file
	require.Len(t, facts, 4)
	kinds := map[string]string{}
	for _, f := range facts {
		kinds[f.Kind+":"+f.Value] = f.Why
	}
	assert.Contains(t, kinds, "no_tools:")
	assert.Contains(t, kinds, "no_param:seed")
	assert.Contains(t, kinds, "no_param:top_p")
	assert.Contains(t, kinds, "context_window:4096")

	assert.Empty(t, s.Facts("ollama", "llama3", now.Add(Recheck+time.Hour)), "facts are asked again after 30 days")
}

func TestForget(t *testing.T) {
	path := Path(t.TempDir())
	s := Open(path)
	now := time.Now()
	for _, k := range []string{"ollama/llama3", "api/llama3", "openai/gpt-5"} {
		p, m, _ := cut(k)
		_, err := s.Learn(p, m, Fact{Kind: NoTools, Why: "x", At: now})
		require.NoError(t, err)
	}
	gone, err := s.Forget("llama3")
	require.NoError(t, err)
	assert.Equal(t, []string{"api/llama3", "ollama/llama3"}, gone, "a bare name, under any provider")
	gone, err = s.Forget("OpenAI/GPT-5")
	require.NoError(t, err)
	assert.Equal(t, []string{"openai/gpt-5"}, gone)
	gone, err = s.Forget("nothing")
	require.NoError(t, err)
	assert.Empty(t, gone)
	assert.Empty(t, Open(path).All())
}

func TestBrokenFile(t *testing.T) {
	path := Path(t.TempDir())
	require.NoError(t, os.WriteFile(path, []byte("not = [toml"), 0o600))
	s := Open(path)
	assert.Empty(t, s.Facts("a", "b", time.Now()), "a broken file teaches nothing")
	_, err := s.Learn("a", "b", Fact{Kind: NoStream, Why: "x", At: time.Now()})
	require.NoError(t, err, "the next fact rewrites it")
	assert.Len(t, Open(path).Facts("a", "b", time.Now()), 1)
	var nilStore *Store
	assert.Nil(t, nilStore.Facts("a", "b", time.Now()))
	fresh, err := nilStore.Learn("a", "b", Fact{})
	assert.False(t, fresh)
	assert.NoError(t, err)
	_, err = Open(filepath.Join(path, "x", "y")).Learn("a", "b", Fact{Kind: NoTools, At: time.Now()})
	assert.Error(t, err, "a folder that can't be made")
}

func cut(k string) (string, string, bool) {
	for i := range k {
		if k[i] == '/' {
			return k[:i], k[i+1:], true
		}
	}
	return "", k, false
}
