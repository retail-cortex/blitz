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

package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReflectionSuggested(t *testing.T) {
	tests := []struct {
		name string
		r    Reflection
		want Suggestion
		ok   bool
	}{
		{"nothing to learn", Reflection{}, Suggestion{}, false},
		{"below the threshold", Reflection{Patterns: 4, Score: 4, Threshold: 5}, Suggestion{}, false},
		{"at the threshold", Reflection{Patterns: 3, Score: 5, Threshold: 5}, Suggestion{Kind: SuggestReflect, Count: 3}, true},
		{"turned off", Reflection{Patterns: 9, Score: 12}, Suggestion{}, false},
		{"proposals first", Reflection{Patterns: 9, Proposals: 2}, Suggestion{Kind: SuggestReflectReview, Count: 2}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := tt.r.Suggested()
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}
