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

package tools

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRememberTool(t *testing.T) {
	type saved struct{ kind, text string }
	tests := []struct {
		name    string
		saver   func(*[]saved) NoteSaver
		args    map[string]any
		want    []saved
		wantOut string
		wantErr string
	}{
		{
			name: "off without a saver",
			args: map[string]any{"note": "x"}, wantErr: "turned off",
		},
		{
			name: "a fact by default",
			saver: func(s *[]saved) NoteSaver {
				return func(_ context.Context, kind, text string) (string, error) {
					*s = append(*s, saved{kind, text})
					return "20260929-note", nil
				}
			},
			args: map[string]any{"note": "tests run with bazel"}, want: []saved{{"fact", "tests run with bazel"}}, wantOut: "20260929-note",
		},
		{
			name: "a preference",
			saver: func(s *[]saved) NoteSaver {
				return func(_ context.Context, kind, text string) (string, error) {
					*s = append(*s, saved{kind, text})
					return "n", nil
				}
			},
			args: map[string]any{"note": "short replies", "kind": "preference"}, want: []saved{{"preference", "short replies"}}, wantOut: "n",
		},
		{
			name: "the saver's error",
			saver: func(*[]saved) NoteSaver {
				return func(context.Context, string, string) (string, error) { return "", errors.New("a note is a fact") }
			},
			args: map[string]any{"note": "x", "kind": "rumour"}, wantErr: "a note is a fact",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Registry{}
			var got []saved
			if tt.saver != nil {
				r.SetNoteSaver(tt.saver(&got))
			}
			out := runTool(t, toolOf(t)(NewRememberTool(r)), tt.args)
			if tt.wantErr != "" {
				assert.Contains(t, errOf(out), tt.wantErr)
				return
			}
			assert.Empty(t, errOf(out))
			assert.Equal(t, tt.wantOut, out["saved"])
			assert.Equal(t, tt.want, got)
		})
	}
}
