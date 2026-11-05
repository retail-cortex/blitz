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

package browsertest

import (
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// As Chrome does, a navigation is answered once the browser decides on
// its request: continued, the page loads; failed, the answer is the error
// and nothing loads (N26 engine-sub-1 #11).
func TestNavigateWaitsForTheDecision(t *testing.T) {
	for _, tc := range []struct {
		decision, errorText string
		loads               bool
	}{
		{"Fetch.continueRequest", "", true},
		{"Fetch.failRequest", "net::ERR_BLOCKED_BY_CLIENT", false},
	} {
		t.Run(tc.decision, func(t *testing.T) {
			p := New(t)
			ws, _, err := websocket.DefaultDialer.Dial(p.URL, nil)
			require.NoError(t, err)
			defer ws.Close()
			require.NoError(t, ws.SetReadDeadline(time.Now().Add(2*time.Second)))
			read := func() map[string]any {
				var m map[string]any
				require.NoError(t, ws.ReadJSON(&m))
				return m
			}
			require.NoError(t, ws.WriteJSON(map[string]any{"id": 1, "method": "Page.navigate", "params": map[string]any{"url": "https://away.example/"}}))
			paused := read()
			require.Equal(t, "Fetch.requestPaused", paused["method"], "no answer before the decision")
			id := paused["params"].(map[string]any)["requestId"]
			require.NoError(t, ws.WriteJSON(map[string]any{"id": 2, "method": tc.decision, "params": map[string]any{"requestId": id}}))
			var answer map[string]any
			var events []string
			for answer == nil || tc.loads && len(events) < 4 {
				m := read()
				switch {
				case m["id"] == float64(1):
					answer = m["result"].(map[string]any)
				case m["method"] != nil:
					events = append(events, m["method"].(string))
				}
			}
			if tc.errorText != "" {
				assert.Equal(t, tc.errorText, answer["errorText"])
			} else {
				assert.NotContains(t, answer, "errorText")
			}
			assert.Equal(t, tc.loads, len(events) > 0)
			p.mu.Lock()
			url := p.page.URL
			p.mu.Unlock()
			assert.Equal(t, tc.loads, url == "https://away.example/")
		})
	}
}
