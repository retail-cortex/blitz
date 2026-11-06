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
	"encoding/json"
	"fmt"
	"iter"
	"strings"
	"sync"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/agents"
	cpsession "github.com/retail-cortex/blitz/pkg/engine/session"
	"github.com/retail-cortex/blitz/pkg/engine/skills"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// chattyLLM answers from what it's sent, so two engines with the same
// history get the same answers: a tool call when asked for one, a reply
// naming the prompt otherwise, a summary when asked for one. Its usage
// grows with the request, so compaction comes round.
type chattyLLM struct {
	mu    sync.Mutex
	first []string // each turn's first request, as JSON
	turn  bool     // the next call starts a turn
}

func (*chattyLLM) Name() string { return "gemini-3.8-flash" }

func (m *chattyLLM) GenerateContent(_ context.Context, req *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		chars := 0
		var last *genai.Part
		for _, c := range req.Contents {
			for _, p := range c.Parts {
				chars += len(p.Text) + 200
				last = p
			}
		}
		raw, _ := json.Marshal(req.Contents)
		m.mu.Lock()
		if m.turn {
			m.first, m.turn = append(m.first, string(raw)), false
		}
		m.mu.Unlock()
		var reply *genai.Content
		switch {
		case req.Config == nil || len(req.Config.Tools) == 0: // the summarizer's call
			reply = genai.NewContentFromText(fmt.Sprintf("SUMMARY of %d chars", chars), genai.RoleModel)
		case last != nil && last.FunctionResponse != nil:
			reply = genai.NewContentFromText("listed them", genai.RoleModel)
		case last != nil && strings.Contains(last.Text, "list"):
			reply = &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "list_files", Args: map[string]any{"directory": "."}}}}}
		default:
			reply = genai.NewContentFromText("about "+textOf(last), genai.RoleModel)
		}
		yield(&model.LLMResponse{Content: reply, UsageMetadata: &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: int32(chars / 4), CandidatesTokenCount: 10}}, nil)
	}
}

func textOf(p *genai.Part) string {
	if p == nil {
		return ""
	}
	return p.Text
}

// chattyEngine is an engine on llm and svc that compacts every few turns.
func chattyEngine(t *testing.T, llm model.LLM, svc session.Service) *Engine {
	t.Helper()
	f := newEngineWith(t, fixtureOpts{cfg: func(c *config.Config) {
		c.Context.TokenThreshold = 1500
		c.Context.RetainEvents = 4
	}})
	agentReg, _ := agents.NewRegistry()
	skillProv, _ := skills.NewProvider()
	eng, err := NewEngine(context.Background(), f.cfg, agentReg, skillProv, f.tools, llm, WithSessionService(svc))
	require.NoError(t, err)
	return eng
}

func chat(t *testing.T, eng *Engine, from, to int) {
	t.Helper()
	for i := from; i < to; i++ {
		prompt := fmt.Sprintf("question %d about f%d.go", i, i%7)
		if i%3 == 0 {
			prompt = fmt.Sprintf("list the files, round %d", i)
		}
		_, err := collect(t, eng, "s", prompt)
		require.NoError(t, err, "turn %d", i)
	}
}

func memoryEvents(t *testing.T, svc session.Service) []*session.Event {
	t.Helper()
	got, err := svc.Get(context.Background(), &session.GetRequest{AppName: appName, UserID: "user", SessionID: "s"})
	require.NoError(t, err)
	return finalEvents(got.Session)
}

// A long conversation keeps in memory only what no summary stands in for,
// and every prompt is the one the whole history would make.
func TestLongConversationKeepsWhatPromptsUse(t *testing.T) {
	dir := t.TempDir()
	svc, err := cpsession.NewPersistentService(dir)
	require.NoError(t, err)
	trimmed := &chattyLLM{}
	eng := chattyEngine(t, trimmed, svc)
	chat(t, eng, 0, 60)

	log, err := svc.Log("s")
	require.NoError(t, err)
	inMemory := memoryEvents(t, svc)
	require.Positive(t, svc.Hidden("s"), "compacted, and the summarised events left memory")
	assert.Len(t, inMemory, len(log)-svc.Hidden("s"))
	assert.Less(t, len(inMemory), len(log)/2, "memory %d of %d events", len(inMemory), len(log))
	assert.Equal(t, len(log), eng.EventCount(context.Background(), "s"), "counted as the log counts, for /rewind")

	// The same history, whole, in a store that keeps everything.
	whole := session.InMemoryService()
	created, err := whole.Create(context.Background(), &session.CreateRequest{AppName: appName, UserID: "user", SessionID: "s"})
	require.NoError(t, err)
	for _, ev := range log {
		require.NoError(t, whole.AppendEvent(context.Background(), created.Session, ev))
	}
	full := &chattyLLM{}
	ref := chattyEngine(t, full, whole)

	for i := 60; i < 75; i++ {
		trimmed.turn, full.turn = true, true
		chat(t, eng, i, i+1)
		chat(t, ref, i, i+1)
		require.Len(t, trimmed.first, i-59)
		require.Len(t, full.first, i-59)
		require.Equal(t, full.first[i-60], trimmed.first[i-60], "turn %d's prompt", i)
	}
}

// /rewind counts events in the log: after the summarised ones left memory
// it still summarises from, and cuts at, the place it names.
func TestRewindAfterSummarisedEventsLeft(t *testing.T) {
	ctx := context.Background()
	svc, err := cpsession.NewPersistentService(t.TempDir())
	require.NoError(t, err)
	eng := chattyEngine(t, &chattyLLM{}, svc)
	chat(t, eng, 0, 30)
	require.Positive(t, svc.Hidden("s"))

	log, err := svc.Log("s")
	require.NoError(t, err)
	at := -1
	for i, ev := range log {
		if isUserTurnStart(ev) {
			at = i
		}
	}
	require.Positive(t, at)
	res, err := eng.CompactAt(ctx, "s", "", at, true)
	require.NoError(t, err)
	assert.Positive(t, res.EventsCompacted)

	require.NoError(t, eng.TruncateSession(ctx, "s", at))
	assert.Equal(t, at, eng.EventCount(ctx, "s"))
	log, err = svc.Log("s")
	require.NoError(t, err)
	assert.Len(t, log, at)
}
