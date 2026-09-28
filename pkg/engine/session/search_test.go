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

package session

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSearchTerms(t *testing.T) {
	got := SearchTerms(`retry "circuit breaker" Retry  backoff`)
	require.Equal(t, []string{"circuit breaker", "retry", "backoff"}, got)
}

func TestSearchRanksAndExcerpts(t *testing.T) {
	long := strings.Repeat("é", 400) + " the Circuit Breaker opens " + strings.Repeat("x", 400)
	msgs := []Message{
		{Role: "user", Content: "add a circuit breaker"},                  // 0: one term
		{Role: "model", Content: "done: breaker plus retry with backoff"}, // 1: breaker, retry, backoff
		{Role: "user", Content: "/search session breaker retry"},          // 2: an earlier search, skipped
		{Role: "user", Content: "unrelated"},                              // 3
		{Role: "model", Content: long},                                    // 4: one phrase hit
		{Role: "user", Content: "RETRY later"},                            // 5: one term
	}
	got, total := Search(msgs, `"circuit breaker" retry backoff`, 3)
	require.Equal(t, 4, total, "total %d", total)
	// Best first (1 has two terms), then the most recent single hits (5, 4),
	// shown oldest first.
	idx := []int{}
	for _, m := range got {
		idx = append(idx, m.Index)
	}
	require.Equal(t, []int{1, 4, 5}, idx, "indexes %v", idx)
	ex := got[1].Excerpt
	require.True(t, strings.HasPrefix(ex, "…"), "excerpt %q", ex)
	require.True(t, strings.HasSuffix(ex, "…"), "excerpt %q", ex)
	require.Contains(t, ex, "Circuit Breaker opens", "excerpt %q", ex)
	require.True(t, utf8Valid(ex), "excerpt %q", ex)
	require.LessOrEqual(t, len(ex), 700*2, "excerpt too long: %d bytes", len(ex))
	got, total = Search(msgs, "   ", 5)
	require.Nil(t, got, "empty query matched")
	require.Equal(t, 0, total, "empty query matched")
}

func utf8Valid(s string) bool { return strings.ToValidUTF8(s, "�") == s }
