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
	"context"
	"errors"
	"iter"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/breaker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// scripted answers with text unless failing; midStream fails after one
// partial response.
type scripted struct {
	name      string
	mu        sync.Mutex
	failing   bool
	midStream bool
	calls     int
}

func (s *scripted) Name() string { return s.name }
func (s *scripted) set(failing bool) {
	s.mu.Lock()
	s.failing = failing
	s.mu.Unlock()
}
func (s *scripted) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}
func (s *scripted) GenerateContent(ctx context.Context, _ *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		s.mu.Lock()
		s.calls++
		failing, mid := s.failing, s.midStream
		s.mu.Unlock()
		if err := ctx.Err(); err != nil {
			yield(nil, err)
			return
		}
		if mid {
			if !yield(&model.LLMResponse{Partial: true, Content: genai.NewContentFromText("par", genai.RoleModel)}, nil) {
				return
			}
			yield(nil, errors.New("stream reset"))
			return
		}
		if failing {
			yield(nil, errors.New(s.name+" is overloaded (529)"))
			return
		}
		yield(&model.LLMResponse{Content: genai.NewContentFromText("from "+s.name, genai.RoleModel)}, nil)
	}
}

func call(t *testing.T, m model.LLM, ctx context.Context) (text string, served, from string, err error) {
	t.Helper()
	for resp, e := range m.GenerateContent(ctx, &model.LLMRequest{}, false) {
		if e != nil {
			return text, served, from, e
		}
		if resp.Content != nil {
			text += resp.Content.Parts[0].Text
		}
		served = resp.ModelVersion
		from, _ = resp.CustomMetadata[FallbackFromKey].(string)
	}
	return
}

func chainOf(t *testing.T, ms ...*scripted) (*fallbackModel, *fakeClock) {
	t.Helper()
	var chain []model.LLM
	for _, m := range ms {
		chain = append(chain, m)
	}
	f := newFallbackModel(chain).(*fallbackModel)
	clk := &fakeClock{t: time.Unix(0, 0)}
	for _, h := range f.health {
		h.SetClock(clk.now)
	}
	return f, clk
}

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func TestFallbackTakesOverThenPrimaryReturns(t *testing.T) {
	primary, backup := &scripted{name: "gemini-x", failing: true}, &scripted{name: "claude-y"}
	f, clk := chainOf(t, primary, backup)
	ctx := context.Background()

	text, served, from, err := call(t, f, ctx)
	require.NoError(t, err, "first call: %q served=%q from=%q err=%v", text, served, from, err)
	require.Equal(t, "from claude-y", text, "first call: %q served=%q from=%q err=%v", text, served, from, err)
	require.Equal(t, "claude-y", served, "first call: %q served=%q from=%q err=%v", text, served, from, err)
	require.Equal(t, "gemini-x", from, "first call: %q served=%q from=%q err=%v", text, served, from, err)
	// The primary is paused: the next call goes straight to the backup.
	_, _, _, err = call(t, f, ctx)
	require.NoError(t, err, "primary retried during its cooldown (%d calls), err", primary.count())
	require.Equal(t, 1, primary.count(), "primary retried during its cooldown (%d calls), err %v", primary.count(), err)
	// After the cooldown the primary gets a trial and takes over again.
	primary.set(false)
	clk.advance(breaker.InitialCooldown)
	text, _, from, _ = call(t, f, ctx)
	require.Equal(t, "from gemini-x", text, "primary not back: %q from=%q calls=%d", text, from, primary.count())
	require.Equal(t, "", from, "primary not back: %q from=%q calls=%d", text, from, primary.count())
	require.Equal(t, 2, primary.count(), "primary not back: %q from=%q calls=%d", text, from, primary.count())
	require.Equal(t, "gemini-x", f.Name(), "name %q models %v", f.Name(), f.Models())
	require.Equal(t, "gemini-x,claude-y", strings.Join(f.Models(), ","), "name %q models %v", f.Name(), f.Models())
}

func TestNoFallbackAfterOutputOrOnCancel(t *testing.T) {
	primary, backup := &scripted{name: "p", midStream: true}, &scripted{name: "b"}
	f, _ := chainOf(t, primary, backup)
	_, _, _, err := call(t, f, context.Background())
	require.Error(t, err, "mid-stream failure")
	require.Contains(t, err.Error(), "stream reset", "mid-stream failure: %v", err)
	require.Equal(t, 0, backup.count(), "fell back after part of the answer was out")

	primary2, backup2 := &scripted{name: "p"}, &scripted{name: "b"}
	f2, _ := chainOf(t, primary2, backup2)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, _, err = call(t, f2, ctx)
	require.ErrorIs(t, err, context.Canceled, "cancelled call: %v", err)
	require.Equal(t, 0, backup2.count(), "fell back on cancellation")
	ok, _ := f2.health[0].Allow()
	require.True(t, ok, "cancellation counted against the primary")
}

func TestAllModelsFailing(t *testing.T) {
	a, b := &scripted{name: "a", failing: true}, &scripted{name: "b", failing: true}
	f, _ := chainOf(t, a, b)
	_, _, _, err := call(t, f, context.Background())
	require.Error(t, err, "err =")
	require.Contains(t, err.Error(), "every model failed", "err = %v", err)
	require.Contains(t, err.Error(), "a is overloaded", "err = %v", err)
	require.Contains(t, err.Error(), "b is overloaded", "err = %v", err)
	// Every breaker is open now; the next call still tries (all of them)
	// instead of failing without an attempt.
	b.set(false)
	text, _, _, err := call(t, f, context.Background())
	require.NoError(t, err, "with every breaker open: %q", text)
	require.Equal(t, "from b", text, "with every breaker open: %q %v", text, err)
}

// A half-open trial cancelled midway must not leave the model skipped
// for good.
func TestCancelledTrialIsReleased(t *testing.T) {
	primary, backup := &scripted{name: "p", failing: true}, &scripted{name: "b"}
	f, clk := chainOf(t, primary, backup)
	call(t, f, context.Background()) // opens the primary
	clk.advance(breaker.InitialCooldown)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	call(t, f, ctx) // trial cancelled
	primary.set(false)
	text, _, _, _ := call(t, f, context.Background())
	require.Equal(t, "from p", text, "primary still skipped after an abandoned trial: %q", text)
}

func TestParseModelRef(t *testing.T) {
	cases := []struct{ ref, def, p, n string }{
		{"anthropic/claude-sonnet-5", "gemini", "anthropic", "claude-sonnet-5"},
		{"Gemini/gemini-3.8-flash", "openai", "gemini", "gemini-3.8-flash"},
		{"gemini-3.5-flash-lite", "gemini", "gemini", "gemini-3.5-flash-lite"},
		{"meta-llama/llama-4", "openai", "openai", "meta-llama/llama-4"},        // not a provider prefix
		{"openai/anthropic/claude-3", "openai", "openai", "anthropic/claude-3"}, // OpenRouter, explicit
		{"ollama/qwen2.5-coder:7b", "gemini", "ollama", "qwen2.5-coder:7b"},
		{"anthropic/", "gemini", "gemini", "anthropic/"},
	}
	for _, c := range cases {
		p, n := ParseModelRef(c.ref, c.def)
		assert.Equal(t, c.p, p, "%q: (%s, %s), want (%s, %s)", c.ref, p, n, c.p, c.n)
		assert.Equal(t, c.n, n, "%q: (%s, %s), want (%s, %s)", c.ref, p, n, c.p, c.n)
	}
}

func TestOllamaDoesNotUseOpenAIDefaults(t *testing.T) {
	d := config.DefaultConfig().LLM.OpenAI
	d.APIKey = "sk-openai-secret"
	key, url := openAICompatEndpoint(d, "ollama")
	require.Equal(t, defaultOllamaBaseURL, url, "ollama with default config: key %q url %q", key, url)
	require.Equal(t, "ollama", key, "ollama with default config: key %q url %q", key, url)
	d.BaseURL = "http://gpu-box:11434/v1"
	_, url = openAICompatEndpoint(d, "ollama")
	require.Equal(t, "http://gpu-box:11434/v1", url, "configured base_url ignored: %q", url)
	key, url = openAICompatEndpoint(config.DefaultConfig().LLM.OpenAI, "openai")
	require.Equal(t, defaultOpenAIBaseURL, url, "openai: %q %q", key, url)
	require.NotEqual(t, "", key, "openai: %q %q", key, url)
}

// Across providers, through the real factory: the primary (OpenAI API)
// keeps failing, the Anthropic fallback answers.
func TestNewModelBuildsACrossProviderChain(t *testing.T) {
	oai, oaiCalls := flaky(t, 100, 500, openAIOK)
	ant, _ := flaky(t, 0, 200, message("end_turn", `{"type":"text","text":"ok"}`))
	cfg := config.DefaultConfig()
	cfg.LLM.Provider, cfg.LLM.OpenAI.BaseURL, cfg.LLM.OpenAI.APIKey = "openai", oai.URL+"/v1", "sk-test"
	cfg.LLM.Anthropic.BaseURL, cfg.LLM.Anthropic.APIKey, cfg.LLM.Anthropic.Fallbacks = ant.URL, "sk-ant-test", "off"
	cfg.LLM.MaxRetries = 0
	cfg.LLM.FallbackModels = []string{"anthropic/claude-sonnet-5", "nosuchprovider/x"}

	m, err := NewModel(context.Background(), cfg, "gpt-test")
	require.NoError(t, err)
	fm, ok := m.(*fallbackModel)
	require.True(t, ok, "chain = %T %v", m, fm)
	require.Equal(t, "gpt-test,claude-sonnet-5,nosuchprovider/x", strings.Join(fm.Models(), ","), "chain = %T %v", m, fm)
	text, err := generateText(t, m)
	require.NoError(t, err, "got %q, %v after %d primary calls", text, err, oaiCalls.Load())
	require.Equal(t, "ok", text, "got %q, %v after %d primary calls", text, err, oaiCalls.Load())
	require.Equal(t, int32(1), oaiCalls.Load(), "got %q, %v after %d primary calls", text, err, oaiCalls.Load())
}

func TestEngineNoticesFallbackOnceAndRecovery(t *testing.T) {
	primary, backup := &scripted{name: "gemini-3.8-flash", failing: true}, &scripted{name: "claude-sonnet-5"}
	var notices []string
	f := newEngineWith(t, fixtureOpts{opts: []Option{WithNotice(func(s string) { notices = append(notices, s) })}})
	chain, clk := chainOf(t, primary, backup)
	require.NoError(t, f.eng.SetModel(context.Background(), chain))
	for range 2 {
		_, err := collect(t, f.eng, "s", "hi")
		require.NoError(t, err)
	}
	primary.set(false)
	clk.advance(breaker.InitialCooldown)
	_, err := collect(t, f.eng, "s", "hi")
	require.NoError(t, err)
	require.Len(t, notices, 2, "notices = %q", notices)
	require.Contains(t, notices[0], "claude-sonnet-5", "notices = %q", notices)
	require.Contains(t, notices[1], "answering again", "notices = %q", notices)
}

func TestEngineSendsFallbackNoticesToTheTurn(t *testing.T) {
	primary, backup := &scripted{name: "gemini-3.8-flash", failing: true}, &scripted{name: "claude-sonnet-5"}
	var engineNotices, turnNotices []string
	f := newEngineWith(t, fixtureOpts{opts: []Option{WithNotice(func(s string) { engineNotices = append(engineNotices, s) })}})
	chain, _ := chainOf(t, primary, backup)
	require.NoError(t, f.eng.SetModel(context.Background(), chain))
	ctx := WithTurnNotices(context.Background(), func(s string) { turnNotices = append(turnNotices, s) })
	require.NoError(t, f.eng.Execute(ctx, "s", "hi", func(*session.Event) error { return nil }))
	require.Len(t, turnNotices, 1, "turn notices = %q", turnNotices)
	require.Contains(t, turnNotices[0], "claude-sonnet-5")
	require.Empty(t, engineNotices, "the turn's notice also went to the engine's sink")
}

// Breakers are asked just before each attempt: when the primary answers a
// trial, the backup's own trial must not stay reserved (it would then be
// skipped for good once the primary fails again).
func TestUnusedBackupTrialIsNotReserved(t *testing.T) {
	primary, backup := &scripted{name: "p", failing: true}, &scripted{name: "b", failing: true}
	f, clk := chainOf(t, primary, backup)
	call(t, f, context.Background()) // both fail: both open
	clk.advance(breaker.InitialCooldown)
	primary.set(false)
	backup.set(false)
	text, _, _, _ := call(t, f, context.Background())
	require.Equal(t, "from p", text, "primary trial: %q", text)
	primary.set(true)
	text, _, _, err := call(t, f, context.Background())
	require.Equal(t, "from b", text, "backup not used after the primary failed again: %q %v", text, err)
}

// A chain of one model is that model, and no model gets no settings.
func TestSingleModelChain(t *testing.T) {
	m := NewMockLLM("only")
	assert.Same(t, m, newFallbackModel([]model.LLM{m}))
	assert.Nil(t, withModelSettings(nil, "gemini"))
}
