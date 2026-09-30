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

package tui

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
)

func TestFollowRun(t *testing.T) {
	tests := []struct {
		name   string
		follow func(ctx context.Context, on func(api.Event)) (api.TurnResult, error)
		signal bool
		want   []string
	}{
		{"to its end", func(_ context.Context, on func(api.Event)) (api.TurnResult, error) {
			on(api.Event{Text: &api.Text{Text: "working on it"}})
			return api.TurnResult{}, nil
		}, false, []string{"Following bg-4", "working on it", "bg-4 has ended"}},
		{"failed", func(context.Context, func(api.Event)) (api.TurnResult, error) {
			return api.TurnResult{}, errors.New("model unavailable")
		}, false, []string{"Following bg-4: model unavailable"}},
		{"stopped following", func(ctx context.Context, _ func(api.Event)) (api.TurnResult, error) {
			<-ctx.Done()
			return api.TurnResult{}, ctx.Err()
		}, true, []string{"Stopped following bg-4", "blitz attach bg-4"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newTestApp(t, nil)
			app.Follow, app.FollowID = tt.follow, "bg-4"
			app.Input = NewLineReader(strings.NewReader("/exit\n"), io.Discard)
			sigs := make(chan os.Signal, 1)
			app.Interrupts = sigs
			if tt.signal {
				sigs <- os.Interrupt
			}
			out := captureStdout(t, func() { assert.NoError(t, RunREPL(context.Background(), app)) })
			for _, w := range tt.want {
				assert.Contains(t, out, w)
			}
			assert.Nil(t, app.Follow, "followed once")
		})
	}
}
