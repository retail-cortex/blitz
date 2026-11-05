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
	"fmt"
	"iter"
	"log/slog"
	"strings"
	"sync"

	"github.com/retail-cortex/blitz/pkg/engine/breaker"
	"google.golang.org/adk/v2/model"
)

// FallbackFromKey is set in a response's CustomMetadata when a fallback
// model answered; its value is the name of the model that failed first.
const FallbackFromKey = "blitz_fallback_from"

// FallbackReasonKey is set beside FallbackFromKey: the primary's error
// that made the chain move on (this call's, or the one that opened its
// breaker), so a broken key or a bad model name isn't hidden behind the
// fallback.
const FallbackReasonKey = "blitz_fallback_reason"

// maxFallbackReason caps the primary's error in the notice: provider
// errors can carry a whole response body.
const maxFallbackReason = 300

// modelFailThreshold is one failure: SDK retries have already run by the
// time a call fails, so one is enough to prefer the next model for a
// while.
const modelFailThreshold = 1

// fallbackModel tries models in order (llm.fallback_models). A model that
// fails is skipped for a cooldown (its circuit breaker), so a provider
// outage doesn't cost a failed call on every step; the primary is tried
// again when its cooldown ends.
//
// It moves on for any error a model returns before producing anything,
// client errors included (a 400, a broken key, an unknown model, a context
// too long, an organization's budget refusal): the owner's decision
// (MDL-61a), with the primary's error named in the notice. Once output has
// been yielded (streamed text is on screen) an error ends the call, as it
// would without fallbacks. Cancellation never moves on.
type fallbackModel struct {
	chain  []model.LLM
	health []*breaker.Breaker

	mu sync.Mutex
	// lastErr is, by model, the error it last failed with before
	// producing anything: the reason named while its breaker is open.
	lastErr []error
}

func newFallbackModel(chain []model.LLM) model.LLM {
	if len(chain) == 1 {
		return chain[0]
	}
	f := &fallbackModel{chain: chain, health: make([]*breaker.Breaker, len(chain)), lastErr: make([]error, len(chain))}
	for i := range chain {
		f.health[i] = breaker.New(modelFailThreshold)
	}
	return f
}

// Name is the primary model's name: the one configured and shown.
func (f *fallbackModel) Name() string { return f.chain[0].Name() }

func (f *fallbackModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		var errs []error
		// First pass: models whose breakers allow a call. Allow is asked
		// just before each attempt because it reserves a half-open trial.
		// If every breaker was open, try them all rather than fail unasked.
		for _, force := range []bool{false, true} {
			tried := false
			for i, m := range f.chain {
				if !force {
					if ok, _ := f.health[i].Allow(); !ok {
						continue
					}
				}
				tried = true
				done, err := f.try(ctx, i, m, req, stream, yield)
				if done {
					return
				}
				f.setLastErr(i, err)
				errs = append(errs, fmt.Errorf("%s: %w", m.Name(), err))
			}
			if tried {
				break
			}
		}
		yield(nil, fmt.Errorf("every model failed: %w", errors.Join(errs...)))
	}
}

// try runs one model. done means the call is over (answered, failed after
// producing output, cancelled, or the consumer stopped); otherwise err is
// why this model failed before producing anything.
func (f *fallbackModel) try(ctx context.Context, i int, m model.LLM, req *model.LLMRequest, stream bool, yield func(*model.LLMResponse, error) bool) (done bool, err error) {
	produced := false
	for resp, err := range m.GenerateContent(ctx, req, stream) {
		if err != nil {
			switch {
			case ctx.Err() != nil || errors.Is(err, context.Canceled):
				f.health[i].Abandon() // no verdict on the model
			case produced: // too late to switch: part of the answer is out
				f.health[i].Failure()
			default:
				f.health[i].Failure()
				slog.WarnContext(ctx, "model failed; trying the next fallback", "model", m.Name(), "error", err)
				return false, err
			}
			yield(nil, err)
			return true, nil
		}
		produced = true
		if i > 0 && resp != nil {
			markFallback(resp, m.Name(), f.chain[0].Name(), f.reason())
		}
		if !yield(resp, nil) {
			f.health[i].Success() // it was answering; the consumer stopped
			return true, nil
		}
	}
	f.health[i].Success()
	return true, nil
}

// setLastErr records why model i failed.
func (f *fallbackModel) setLastErr(i int, err error) {
	f.mu.Lock()
	f.lastErr[i] = err
	f.mu.Unlock()
}

// reason is the primary's last error as one short line, or "" if none is
// known.
func (f *fallbackModel) reason() string {
	f.mu.Lock()
	err := f.lastErr[0]
	f.mu.Unlock()
	if err == nil {
		return ""
	}
	s := strings.Join(strings.Fields(err.Error()), " ")
	if r := []rune(s); len(r) > maxFallbackReason {
		s = string(r[:maxFallbackReason]) + "…"
	}
	return s
}

// markFallback records which model answered, so usage is priced by it,
// and why the primary didn't, so the user can be told.
func markFallback(resp *model.LLMResponse, served, primary, reason string) {
	if resp.ModelVersion == "" {
		resp.ModelVersion = served
	}
	if resp.CustomMetadata == nil {
		resp.CustomMetadata = map[string]any{}
	}
	resp.CustomMetadata[FallbackFromKey] = primary
	if reason != "" {
		resp.CustomMetadata[FallbackReasonKey] = reason
	}
}
