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

	"google.golang.org/adk/v2/model"
)

// ErrModelUnavailable is the error of every call to a model that couldn't
// be built.
var ErrModelUnavailable = errors.New("the model isn't available")

// unavailableModel stands in for a model that couldn't be built (no
// credentials, a setting the provider refuses): every call fails with
// why, so a turn fails instead of getting an answer from nowhere.
type unavailableModel struct {
	name string
	why  string
}

// NewUnavailableModel is a model named name whose every call fails with
// ErrModelUnavailable and why (which must hold no secrets: it reaches the
// user as the turn's error).
func NewUnavailableModel(name, why string) model.LLM {
	return &unavailableModel{name: name, why: why}
}

func (m *unavailableModel) Name() string { return m.name }

func (m *unavailableModel) GenerateContent(context.Context, *model.LLMRequest, bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		yield(nil, fmt.Errorf("%w: %s", ErrModelUnavailable, m.why))
	}
}
