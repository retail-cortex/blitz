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
	"strings"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Metrics (spec_parity_027 PAR-MOD-07): counters of what Blitz does,
// exported with traces and logs when telemetry is on. Without it the global
// meter does nothing, so recording costs next to nothing.

var (
	metricsOnce sync.Once
	instruments struct {
		sessions  metric.Int64Counter
		turns     metric.Int64Counter
		tokens    metric.Int64Counter
		cost      metric.Float64Counter
		toolCalls metric.Int64Counter
		approvals metric.Int64Counter
	}
)

// meter makes the instruments on first use, from the global meter
// provider then in place.
func meter() {
	metricsOnce.Do(func() {
		m := otel.Meter(ServiceName)
		instruments.sessions, _ = m.Int64Counter("blitz.session.count", metric.WithDescription("Sessions started or resumed"), metric.WithUnit("{session}"))
		instruments.turns, _ = m.Int64Counter("blitz.turn.count", metric.WithDescription("Prompts run, by agent and outcome"), metric.WithUnit("{turn}"))
		instruments.tokens, _ = m.Int64Counter("blitz.token.usage", metric.WithDescription("Model tokens, by model and type"), metric.WithUnit("{token}"))
		instruments.cost, _ = m.Float64Counter("blitz.cost.usage", metric.WithDescription("Estimated model cost, by model"), metric.WithUnit("USD"))
		instruments.toolCalls, _ = m.Int64Counter("blitz.tool.calls", metric.WithDescription("Tool calls, by tool and outcome"), metric.WithUnit("{call}"))
		instruments.approvals, _ = m.Int64Counter("blitz.approval.decisions", metric.WithDescription("Approval decisions, by action kind and how they were made"), metric.WithUnit("{decision}"))
	})
}

// RecordSession counts a session started (reason: startup, new, resume…).
func RecordSession(ctx context.Context, reason string) {
	meter()
	instruments.sessions.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", reason)))
}

// RecordTurn counts a prompt run by agent: ok, error, blocked or limit.
func RecordTurn(ctx context.Context, agent, outcome string) {
	meter()
	instruments.turns.Add(ctx, 1, metric.WithAttributes(attribute.String("agent", agent), attribute.String("outcome", outcome)))
}

// RecordTokens counts a model call's tokens (input, output, cached input)
// and its estimated cost (when priced).
func RecordTokens(ctx context.Context, model string, input, output, cached int64, costUSD float64, priced bool) {
	meter()
	for _, t := range []struct {
		kind string
		n    int64
	}{{"input", input}, {"output", output}, {"cached_input", cached}} {
		if t.n > 0 {
			instruments.tokens.Add(ctx, t.n, metric.WithAttributes(attribute.String("model", model), attribute.String("type", t.kind)))
		}
	}
	if priced && costUSD > 0 {
		instruments.cost.Add(ctx, costUSD, metric.WithAttributes(attribute.String("model", model)))
	}
}

// RecordToolCall counts a tool call: ok or error.
func RecordToolCall(ctx context.Context, tool, outcome string) {
	meter()
	instruments.toolCalls.Add(ctx, 1, metric.WithAttributes(attribute.String("tool", tool), attribute.String("outcome", outcome)))
}

// RecordApproval counts an approval decision by action kind and how it was
// made (its first word: user, rule-allow, mode-bypass, hook-deny…).
func RecordApproval(ctx context.Context, kind, decision string) {
	meter()
	how, _, _ := strings.Cut(decision, " ")
	instruments.approvals.Add(ctx, 1, metric.WithAttributes(attribute.String("kind", kind), attribute.String("decision", how)))
}
