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
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/redact"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace/noop"
)

// A recording span's traceparent parses back to its context; a span that
// isn't recording (telemetry off) has none.
func TestTraceparent(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	_, span := tp.Tracer("t").Start(context.Background(), "s")
	defer span.End()
	tp1 := Traceparent(span)
	require.NotEmpty(t, tp1)
	sc, ok := ParseTraceparent(tp1)
	require.True(t, ok)
	assert.Equal(t, span.SpanContext().TraceID(), sc.TraceID())
	assert.True(t, sc.IsRemote())

	_, off := noop.NewTracerProvider().Tracer("t").Start(context.Background(), "s")
	assert.Empty(t, Traceparent(off))
	_, ok = ParseTraceparent("")
	assert.False(t, ok)
	_, ok = ParseTraceparent("garbage")
	assert.False(t, ok)
}

// End marks a cancelled span as cancelled, not failed.
func TestEndCancelled(t *testing.T) {
	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))
	_, span := tp.Tracer("t").Start(context.Background(), "s")
	End(span, context.Canceled)
	got := mem.GetSpans()
	require.Len(t, got, 1)
	assert.Contains(t, got[0].Attributes, attribute.Bool("cancelled", true))
	assert.Empty(t, got[0].Events, "no error recorded")
}

// Secrets are masked inside slices and maps too; other types pass through.
func TestRedactValueNested(t *testing.T) {
	r := redact.New(secret)
	v := redactValue(r, attribute.SliceValue(attribute.StringValue("k="+secret), attribute.IntValue(3)))
	assert.NotContains(t, v.String(), secret)
	assert.Contains(t, v.String(), "3")
	m := redactValue(r, attribute.MapValue(attribute.String("key", secret)))
	assert.NotContains(t, m.String(), secret)
	assert.Equal(t, attribute.IntValue(7), redactValue(r, attribute.IntValue(7)))
}

// Without capture, the bodies of GenAI content events are dropped.
func TestRedactProcessor(t *testing.T) {
	p := &redactProcessor{r: redact.New(secret)}
	var ev sdklog.Record
	ev.SetEventName("gen_ai.user.message")
	ev.SetBody(attribute.StringValue("prompt with " + secret))
	require.NoError(t, p.OnEmit(context.Background(), &ev))
	assert.Equal(t, attribute.Value{}, ev.Body(), "content event body dropped")

	assert.True(t, p.Enabled(context.Background(), sdklog.EnabledParameters{}))
	assert.NoError(t, p.Shutdown(context.Background()))
	assert.NoError(t, p.ForceFlush(context.Background()))
}

// With capture on and nothing set, content goes to spans and events;
// export failures are handled quietly. A nil Telemetry flushes nothing.
func TestStartTelemetryCapture(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-protobuf")
	}))
	defer srv.Close()
	t.Setenv(captureContentEnv, "")
	tel, err := StartTelemetry(context.Background(), config.TelemetryConfig{Enabled: true, CaptureContent: true, Endpoint: srv.URL}, "test", redact.New())
	require.NoError(t, err)
	defer tel.Shutdown(context.Background())
	assert.Equal(t, "SPAN_AND_EVENT", os.Getenv(captureContentEnv))
	otel.Handle(errors.New("collector down")) // logged at debug, not printed

	var none *Telemetry
	assert.NoError(t, none.Flush(context.Background()))
	none.StartMetrics(nil)
}
