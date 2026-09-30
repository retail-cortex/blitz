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
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/redact"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestMetrics(t *testing.T) {
	var nilT *Telemetry
	nilT.StartMetrics(nil) // telemetry off: nothing to do

	tel := NewTelemetry(config.TelemetryConfig{Enabled: true}, "test", redact.New(), tracetest.NewInMemoryExporter(), &memLogExporter{})
	reader := sdkmetric.NewManualReader()
	tel.StartMetrics(reader)
	t.Cleanup(func() { tel.Shutdown(context.Background()) })

	ctx := context.Background()
	RecordSession(ctx, "startup")
	RecordTurn(ctx, "blitz", "ok")
	RecordTurn(ctx, "blitz", "ok")
	RecordTurn(ctx, "qa", "error")
	RecordTokens(ctx, "gemini-3.8-flash", 100, 20, 30, 0.01, true)
	RecordTokens(ctx, "local", 5, 0, 0, 0, false)
	RecordToolCall(ctx, "read_file", "ok")
	RecordApproval(ctx, "write_file", "rule-allow write(docs/**)")
	RecordApproval(ctx, "write_file", "user")

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))
	sums := map[string]map[string]float64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			sums[m.Name] = map[string]float64{}
			switch d := m.Data.(type) {
			case metricdata.Sum[int64]:
				for _, p := range d.DataPoints {
					sums[m.Name][key(p.Attributes)] += float64(p.Value)
				}
			case metricdata.Sum[float64]:
				for _, p := range d.DataPoints {
					sums[m.Name][key(p.Attributes)] += p.Value
				}
			}
		}
	}
	assert.Equal(t, map[string]float64{"reason=startup": 1}, sums["blitz.session.count"])
	assert.Equal(t, map[string]float64{"agent=blitz,outcome=ok": 2, "agent=qa,outcome=error": 1}, sums["blitz.turn.count"])
	assert.Equal(t, map[string]float64{
		"model=gemini-3.8-flash,type=input": 100, "model=gemini-3.8-flash,type=output": 20, "model=gemini-3.8-flash,type=cached_input": 30,
		"model=local,type=input": 5,
	}, sums["blitz.token.usage"])
	assert.InDelta(t, 0.01, sums["blitz.cost.usage"]["model=gemini-3.8-flash"], 1e-9)
	assert.NotContains(t, sums["blitz.cost.usage"], "model=local", "unpriced: no cost")
	assert.Equal(t, map[string]float64{"outcome=ok,tool=read_file": 1}, sums["blitz.tool.calls"])
	assert.Equal(t, map[string]float64{"decision=rule-allow,kind=write_file": 1, "decision=user,kind=write_file": 1}, sums["blitz.approval.decisions"])
	require.NoError(t, tel.Flush(ctx))
}

// key is a point's attributes, sorted, as text.
func key(set attribute.Set) string {
	s := ""
	for i, kv := range set.ToSlice() {
		if i > 0 {
			s += ","
		}
		s += string(kv.Key) + "=" + kv.Value.String()
	}
	return s
}
