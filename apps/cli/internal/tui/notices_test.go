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
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// noticeBackend has one notice waiting.
type noticeBackend struct {
	api.Backend
	notices []string
}

func (b *noticeBackend) TakeProcessNotices(string) []string {
	n := b.notices
	b.notices = nil
	return n
}

func TestWatchNotices(t *testing.T) {
	old := noticePoll
	noticePoll = 10 * time.Millisecond
	t.Cleanup(func() { noticePoll = old })
	app := newTestApp(t, nil)
	_, err := app.Workspace.NewSession()
	require.NoError(t, err)
	app.Workspace = &noticeBackend{Backend: app.Workspace, notices: []string{"Background process 1 (`make`) exited with code 2"}}

	ctx, stop := watchNotices(context.Background(), app)
	defer stop()
	select {
	case <-ctx.Done():
		assert.True(t, errors.Is(context.Cause(ctx), errNoticeDue))
	case <-time.After(5 * time.Second):
		t.Fatal("the notice didn't interrupt the prompt")
	}
	assert.True(t, app.hasNotices())

	// Nothing waiting: the prompt isn't interrupted.
	quiet, stopQuiet := watchNotices(context.Background(), &App{Workspace: &noticeBackend{Backend: app.Workspace}})
	time.Sleep(50 * time.Millisecond)
	assert.NoError(t, quiet.Err())
	stopQuiet()
}
