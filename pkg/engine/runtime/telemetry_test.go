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
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/session"
	"github.com/retail-cortex/blitz/pkg/observability"
	"github.com/retail-cortex/blitz/pkg/redact"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/genai"
)

type discardLogs struct{}

func (discardLogs) Export(context.Context, []sdklog.Record) error { return nil }
func (discardLogs) Shutdown(context.Context) error                { return nil }
func (discardLogs) ForceFlush(context.Context) error              { return nil }

// testTelemetry is shared by every test in the process: the ADK binds its
// tracer to the first provider installed (see observability.NewTelemetry).
var testTelemetry = sync.OnceValues(func() (*observability.Telemetry, *tracetest.InMemoryExporter) {
	spans := tracetest.NewInMemoryExporter()
	return observability.NewTelemetry(config.TelemetryConfig{Enabled: true}, "test", redact.New(), spans, discardLogs{}), spans
})

// A real run through the ADK must produce one trace rooted at our turn span,
// with the ADK's model and tool spans nested inside, and no file content.
func TestTurnTraceNestsADKSpansWithoutContent(t *testing.T) {
	const content = "TOP-SECRET-FILE-CONTENT"
	f := newEngineWith(t, fixtureOpts{},
		toolCall("read_file", map[string]any{"path": "notes.txt"}),
		textContent("done"))
	f.llm.Usage = &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 100, CandidatesTokenCount: 10}
	require.NoError(t, os.WriteFile(filepath.Join(f.cfg.Tools.WorkspaceDir, "notes.txt"), []byte(content), 0o600))

	tel, spans := testTelemetry()
	spans.Reset()

	got, err := functionResponses(t, f.eng, "s", "read my notes")
	require.NoError(t, err)
	c, _ := got["read_file"]["content"].(string)
	require.Contains(t, c, content, "tool did not run as expected: %v", got)
	tel.Flush(context.Background()) // not Shutdown: it clears the in-memory exporter

	byName := map[string]tracetest.SpanStub{}
	parent := map[string]string{} // span ID -> parent span ID
	idName := map[string]string{}
	for _, s := range spans.GetSpans() {
		t.Run(s.Name, func(t *testing.T) {
			byName[s.Name] = s
			idName[s.SpanContext.SpanID().String()] = s.Name
			parent[s.SpanContext.SpanID().String()] = s.Parent.SpanID().String()
			for _, kv := range s.Attributes {
				assert.NotContains(t, kv.Value.String(), content, "span %q attribute %s carries file content", s.Name, kv.Key)
			}
		})
	}
	t.Logf("spans: %v", idName)
	turn, ok := byName["turn"]
	require.True(t, ok, "no turn span; got %v", idName)
	tool, ok := byName["execute_tool read_file"]
	require.True(t, ok, "no execute_tool span; got %v", idName)
	require.Equal(t, turn.SpanContext.TraceID(), tool.SpanContext.TraceID(), "tool span is in a different trace from the turn")
	// Walk up from the tool span: it must reach the turn span.
	id, hops := tool.SpanContext.SpanID().String(), 0
	for id != turn.SpanContext.SpanID().String() {
		id = parent[id]
		require.NotEmpty(t, id, "the tool span is not nested under the turn span")
		require.LessOrEqual(t, hops, 10, "the tool span is not nested under the turn span")
		hops++
	}
	for _, kv := range tool.Attributes {
		assert.NotEqual(t, attribute.Key("gcp.vertex.agent.tool_call_args"), kv.Key, "tool content attribute %s exported", kv.Key)
		assert.NotEqual(t, attribute.Key("gcp.vertex.agent.tool_response"), kv.Key, "tool content attribute %s exported", kv.Key)
	}
	calls := false
	for _, kv := range turn.Attributes {
		if kv.Key == "model_calls" && kv.Value.AsInt64() == 2 {
			calls = true
		}
	}
	assert.True(t, calls, "turn span model_calls != 2: %v", turn.Attributes)
}

func turnSpans(spans *tracetest.InMemoryExporter) []tracetest.SpanStub {
	var out []tracetest.SpanStub
	for _, s := range spans.GetSpans() {
		if s.Name == "turn" {
			out = append(out, s)
		}
	}
	return out
}

func attr(s tracetest.SpanStub, key string) attribute.Value {
	for _, kv := range s.Attributes {
		if string(kv.Key) == key {
			return kv.Value
		}
	}
	return attribute.Value{}
}

// Turns of a session are separate traces chained by links to the previous
// turn, and the chain continues after a resume in a new process.
func TestTurnsChainAcrossResume(t *testing.T) {
	tel, spans := testTelemetry()
	spans.Reset()
	dir := t.TempDir()

	store, _ := session.NewStorage(dir)
	rec, _ := store.CreateSession("", "chain", "blitz")
	f := newEngineWith(t, fixtureOpts{opts: []Option{WithTurnStore(store)}}, textContent("one"), textContent("two"))
	for _, p := range []string{"first", "second"} {
		t.Run(p, func(t *testing.T) {
			_, err := collect(t, f.eng, rec.ID, p)
			require.NoError(t, err)
		})
	}

	// A later process: new storage and engine, same session.
	store2, _ := session.NewStorage(dir)
	_, err := store2.Load(rec.ID)
	require.NoError(t, err)
	f2 := newEngineWith(t, fixtureOpts{opts: []Option{WithTurnStore(store2)}}, textContent("three"))
	_, err = collect(t, f2.eng, rec.ID, "third")
	require.NoError(t, err)
	tel.Flush(context.Background())

	turns := turnSpans(spans)
	require.Len(t, turns, 3, "want 3 turn spans, got %d", len(turns))
	for i, s := range turns {
		got := attr(s, "gen_ai.conversation.id").AsString()
		assert.Equal(t, rec.ID, got, "turn %d: conversation id %q", i+1, got)
		assert.Equal(t, int64(i+1), attr(s, "turn.index").AsInt64(), "turn %d: turn.index", i+1)
		assert.False(t, s.Parent.IsValid(), "turn %d is not a trace root", i+1)
		if i == 0 {
			assert.Len(t, s.Links, 0, "first turn has links: %v", s.Links)
			continue
		}
		prev := turns[i-1].SpanContext
		assert.NotEqual(t, prev.TraceID(), s.SpanContext.TraceID(), "turn %d shares a trace with the previous turn", i+1)
		assert.Len(t, s.Links, 1, "turn %d does not link to turn %d: %+v", i+1, i, s.Links)
		assert.Equal(t, prev.SpanID(), s.Links[0].SpanContext.SpanID(), "turn %d does not link to turn %d: %+v", i+1, i, s.Links)
		assert.Equal(t, prev.TraceID(), s.Links[0].SpanContext.TraceID(), "turn %d does not link to turn %d: %+v", i+1, i, s.Links)
	}
}

type countingStore struct{ sets int }

func (c *countingStore) LastTurn(string) (string, int) { return "", 0 }
func (c *countingStore) SetLastTurn(string, string, int) error {
	c.sets++
	return nil
}

// With telemetry off spans carry no trace context, so nothing is saved.
func TestTurnNotRecordedWithoutTrace(t *testing.T) {
	store := &countingStore{}
	e := &Engine{turns: store}
	e.recordTurn(context.Background(), "s", trace.SpanFromContext(context.Background()), 1)
	require.Equal(t, 0, store.sets, "turn recorded without a trace")
}
