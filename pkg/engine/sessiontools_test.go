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

package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A saved session's tool calls come back with their results, under the
// prompt they ran for, long text clipped.
func TestSessionTools(t *testing.T) {
	w, _ := openTestWith(t, nil,
		toolCall("list_files", map[string]any{"path": "."}), text("Nothing much."),
		toolCall("read_file", map[string]any{"path": "missing.txt"}), text("No such file."))
	s := newSession(t, w)
	_, err := w.Run(context.Background(), s.ID, api.Turn{Text: "what's here?"}, func(api.Event) {})
	require.NoError(t, err)
	_, err = w.Run(context.Background(), s.ID, api.Turn{Text: "read missing.txt"}, func(api.Event) {})
	require.NoError(t, err)

	tools, err := w.SessionTools(s.ID)
	require.NoError(t, err)
	require.Len(t, tools, 2)
	assert.Equal(t, "list_files", tools[0].Name)
	assert.Equal(t, 0, tools[0].Turn)
	assert.Equal(t, "what's here?", tools[0].Prompt)
	assert.Equal(t, ".", tools[0].Args["path"])
	assert.NotNil(t, tools[0].Result)
	assert.Equal(t, "read_file", tools[1].Name)
	assert.Equal(t, 1, tools[1].Turn)
	assert.Equal(t, "read missing.txt", tools[1].Prompt)

	none, err := w.SessionTools("session-20990101-000000-none")
	require.NoError(t, err)
	assert.Empty(t, none)
	_, err = w.SessionTools("../x")
	assert.Error(t, err)

	long := strings.Repeat("x", maxToolText+10)
	clipped := clipValues(map[string]any{"a": long, "b": []any{long, 3.0}, "c": map[string]any{"d": long}})
	assert.Len(t, clipped["a"], maxToolText+len("…"))
	assert.Equal(t, 3.0, clipped["b"].([]any)[1])
	assert.Len(t, clipped["c"].(map[string]any)["d"], maxToolText+len("…"))
	assert.Nil(t, clipValues(nil))
}
