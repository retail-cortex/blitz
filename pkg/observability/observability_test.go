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

package observability

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/redact"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

const secret = "sk-test-SECRET-123456"

func readLines(t *testing.T, dir string) []map[string]any {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, logPrefix+"*"+logSuffix))
	require.Len(t, files, 1, "want one log file, got %v", files)
	f, err := os.Open(files[0])
	require.NoError(t, err)
	defer f.Close()
	var out []map[string]any
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var m map[string]any
		require.NoError(t, json.Unmarshal(sc.Bytes(), &m), "bad line %q", sc.Text())
		out = append(out, m)
	}
	return out
}

func TestLogFileMasksSecretsAndKeepsEveryRecord(t *testing.T) {
	dir := t.TempDir()
	logger, closer, err := OpenLog(config.LogConfig{Level: "debug", Dir: dir}, redact.New(secret))
	require.NoError(t, err)

	const writers, each = 8, 100 // 800 < queue size: nothing may be dropped
	var wg sync.WaitGroup
	for w := range writers {
		wg.Go(func() {
			for i := range each {
				logger.Info("call failed", "writer", w, "i", i, "error", errors.New("key "+secret+" rejected"))
			}
		})
	}
	wg.Wait()
	logger.Debug("token is " + secret)
	closer.Close()

	lines := readLines(t, dir)
	require.Len(t, lines, writers*each+1, "want %d lines, got %d", writers*each+1, len(lines))
	for _, l := range lines {
		b, _ := json.Marshal(l)
		require.NotContains(t, string(b), secret, "secret written to log: %s", b)
	}
	info, err := os.Stat(dir)
	require.NoError(t, err, "log dir mode = %v,", info.Mode().Perm())
	require.Equal(t, fs.FileMode(0o700), info.Mode().Perm(), "log dir mode = %v, %v", info.Mode().Perm(), err)
}

func TestLogFileNeverBlocksAndReportsDrops(t *testing.T) {
	dir := t.TempDir()
	// A sink whose writer hasn't started: the queue fills and writes must
	// still return immediately.
	s := &fileSink{queue: make(chan []byte, 2), done: make(chan struct{}), dir: dir, now: time.Now}
	start := time.Now()
	for i := range 10 {
		s.Write(fmt.Appendf(nil, `{"n":%d}`+"\n", i))
	}
	require.LessOrEqual(t, time.Since(start), time.Second, "Write blocked on a full queue")
	go s.run()
	s.Close()

	lines := readLines(t, dir)
	require.Len(t, lines, 3, "want 2 records and a drop notice, got %v", lines)
	require.Equal(t, float64(8), lines[2]["count"], "drop notice = %v", lines[2])
	s.Write([]byte("after close\n")) // must not panic
}

func TestLogFilePrunesOldFiles(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, logPrefix+"2000-01-01"+logSuffix)
	other := filepath.Join(dir, "notes.txt")
	for _, p := range []string{old, other} {
		os.WriteFile(p, []byte("x\n"), 0o600)
	}
	logger, closer, err := OpenLog(config.LogConfig{Level: "info", Dir: dir, RetainDays: 7}, redact.New())
	require.NoError(t, err)
	logger.Info("hello")
	closer.Close()
	_, err = os.Stat(old)
	require.ErrorIs(t, err, fs.ErrNotExist, "old log file not pruned")
	_, err = os.Stat(other)
	require.NoError(t, err, "unrelated file removed")
}

func TestLogLevelOffWritesNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	logger, closer, err := OpenLog(config.LogConfig{Level: "off", Dir: dir}, redact.New())
	require.NoError(t, err)
	logger.Error("x")
	closer.Close()
	_, err = os.Stat(dir)
	require.ErrorIs(t, err, fs.ErrNotExist, "log dir created with logging off")
	_, _, err = OpenLog(config.LogConfig{Level: "loud"}, redact.New())
	require.Error(t, err, "unknown level accepted")
}

func TestLogLinesCarryTraceIDs(t *testing.T) {
	dir := t.TempDir()
	logger, closer, _ := OpenLog(config.LogConfig{Level: "info", Dir: dir}, redact.New())
	tp := sdktrace.NewTracerProvider()
	ctx, span := tp.Tracer("t").Start(context.Background(), "op")
	logger.InfoContext(ctx, "inside")
	span.End()
	closer.Close()
	l := readLines(t, dir)[0]
	require.Equal(t, span.SpanContext().TraceID().String(), l["trace_id"], "trace_id missing: %v", l)
}

func exportThrough(t *testing.T, capture bool, attrs ...attribute.KeyValue) []attribute.KeyValue {
	t.Helper()
	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(&filterExporter{next: mem, capture: capture, r: redact.New(secret)}))
	_, span := tp.Tracer("t").Start(context.Background(), "execute_tool read_file")
	span.SetAttributes(attrs...)
	span.AddEvent("e", trace.WithAttributes(attrs...))
	span.End()
	spans := mem.GetSpans()
	require.Len(t, spans, 1, "want 1 span, got %d", len(spans))
	ev := spans[0].Events[0].Attributes
	require.Len(t, ev, len(spans[0].Attributes), "event attributes filtered differently: %v vs %v", ev, spans[0].Attributes)
	return spans[0].Attributes
}

func TestSpansDropContentUnlessCaptured(t *testing.T) {
	in := []attribute.KeyValue{
		attribute.String("gen_ai.tool.name", "read_file"),
		attribute.String("gcp.vertex.agent.tool_call_args", `{"path":".env"}`),
		attribute.String("gcp.vertex.agent.tool_response", "API_KEY="+secret),
		attribute.String("error.message", "bad key "+secret),
		attribute.StringSlice("list", []string{secret}),
	}

	got := map[attribute.Key]string{}
	for _, kv := range exportThrough(t, false, in...) {
		got[kv.Key] = kv.Value.String()
	}
	_, ok := got["gcp.vertex.agent.tool_call_args"]
	require.False(t, ok, "tool args exported without capture")
	_, ok = got["gcp.vertex.agent.tool_response"]
	require.False(t, ok, "tool response exported without capture")
	require.Equal(t, "read_file", got["gen_ai.tool.name"], "tool name lost: %v", got)
	for k, v := range got {
		require.NotContains(t, v, secret, "secret in %s: %s", k, v)
	}

	got = map[attribute.Key]string{}
	for _, kv := range exportThrough(t, true, in...) {
		got[kv.Key] = kv.Value.String()
	}
	require.Equal(t, `{"path":".env"}`, got["gcp.vertex.agent.tool_call_args"], "captured args missing: %v", got)
	v := got["gcp.vertex.agent.tool_response"]
	require.NotEqual(t, "", v, "captured response not masked: %q", v)
	require.NotContains(t, v, secret, "captured response not masked: %q", v)
}

type memLogExporter struct {
	mu   sync.Mutex
	recs []sdklog.Record
}

func (m *memLogExporter) Export(_ context.Context, recs []sdklog.Record) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range recs {
		m.recs = append(m.recs, r.Clone())
	}
	return nil
}
func (m *memLogExporter) Shutdown(context.Context) error   { return nil }
func (m *memLogExporter) ForceFlush(context.Context) error { return nil }

func TestTelemetryLogHandlerMasksSecrets(t *testing.T) {
	spans, logs := tracetest.NewInMemoryExporter(), &memLogExporter{}
	tel := NewTelemetry(config.TelemetryConfig{Enabled: true}, "test", redact.New(secret), spans, logs)
	logger, closer, err := OpenLog(config.LogConfig{Level: "off"}, redact.New(secret), tel.LogHandler())
	require.NoError(t, err)
	ctx, span := Start(context.Background(), "turn")
	logger.WarnContext(ctx, "auth failed with "+secret, "detail", "key="+secret)
	End(span, errors.New("401 for "+secret))
	closer.Close()
	tel.Flush(context.Background())
	got := spans.GetSpans() // read before Shutdown, which clears the exporter
	require.NoError(t, tel.Shutdown(context.Background()))

	require.Len(t, logs.recs, 1, "want 1 exported log record, got %d", len(logs.recs))
	rec := logs.recs[0]
	text := rec.Body().String()
	rec.WalkAttributes(func(kv attribute.KeyValue) bool { text += " " + kv.Value.String(); return true })
	require.NotContains(t, text, secret, "log record not masked: %s", text)
	require.Contains(t, text, "auth failed", "log record not masked: %s", text)
	require.Equal(t, span.SpanContext().TraceID(), rec.TraceID(), "log record not linked to the span")

	require.Len(t, got, 1, "span status not masked: %+v", got)
	require.NotContains(t, got[0].Status.Description, secret, "span status not masked: %+v", got)
}

func TestTelemetryOffIsNil(t *testing.T) {
	tel, err := StartTelemetry(context.Background(), config.TelemetryConfig{}, "v", redact.New())
	require.Nil(t, tel, "got %v, %v", tel, err)
	require.NoError(t, err, "got %v,", tel)
	require.Nil(t, tel.LogHandler(), "nil telemetry must be a no-op")
	require.NoError(t, tel.Shutdown(context.Background()), "nil telemetry must be a no-op")
}

// StartTelemetry's real OTLP/HTTP exporters must post to <endpoint>/v1/traces
// and /v1/logs, and Shutdown must flush what is pending.
func TestStartTelemetryExportsOverOTLPHTTP(t *testing.T) {
	var mu sync.Mutex
	paths := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths[r.URL.Path]++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/x-protobuf")
	}))
	defer srv.Close()

	tel, err := StartTelemetry(context.Background(), config.TelemetryConfig{Enabled: true, Endpoint: srv.URL + "/"}, "test", redact.New())
	require.NoError(t, err)
	logger := slog.New(tel.LogHandler())
	ctx, span := Start(context.Background(), "turn")
	logger.InfoContext(ctx, "hello")
	End(span, nil)
	require.NoError(t, tel.Shutdown(context.Background()))
	mu.Lock()
	defer mu.Unlock()
	require.NotEqual(t, 0, paths["/v1/traces"], "collector received %v", paths)
	require.NotEqual(t, 0, paths["/v1/logs"], "collector received %v", paths)
}

func TestEndpointDefaultsToPlainHTTP(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	got := endpointFor("", "TRACES")
	require.Equal(t, "http://localhost:4318", got, "default = %q", got)
	got = endpointFor("https://otel.example.com/", "TRACES")
	require.Equal(t, "https://otel.example.com", got, "configured = %q", got)
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "https://x/v1/traces")
	got = endpointFor("", "TRACES")
	require.Equal(t, "", got, "env endpoint must be left to the exporter, got %q", got)
}
