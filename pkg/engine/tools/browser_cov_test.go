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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/browser"
	"github.com/retail-cortex/blitz/pkg/engine/browser/browsertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// URLs the browser refuses before asking anyone: none, or without a host.
func TestBrowserToolRefusesBadURLs(t *testing.T) {
	hooks, reqs := decisionHooks(api.DecisionOnce)
	bt := testBrowserTool(t, hooks, nil)
	for _, tt := range []struct{ url, want string }{
		{"", "invalid URL"},
		{"http://[::1", "invalid URL"},
		{"https:///path", "no host"},
	} {
		t.Run(tt.url, func(t *testing.T) {
			out := bt.run(context.Background(), BrowserInput{Action: "navigate", URL: tt.url})
			assert.Contains(t, out.Error, tt.want)
		})
	}
	assert.Empty(t, *reqs, "nothing asked")
}

// A browser that won't start is the action's error, through the tool.
func TestBrowserToolLaunchFails(t *testing.T) {
	hooks, _ := decisionHooks(api.DecisionOnce)
	bt := testBrowserTool(t, hooks, nil)
	bt.launch = func(context.Context, browser.Options) (*browser.Browser, error) {
		return nil, errors.New("no Chrome here")
	}
	tl := toolOf(t)(NewBrowserTool(bt))
	out := runTool(t, tl, map[string]any{"action": "read"})
	assert.Equal(t, "no Chrome here", errOf(out))
	assert.Nil(t, bt.b)
}

// A walkthrough that can't be saved isn't started.
func TestBrowserToolRecordFails(t *testing.T) {
	page := browsertest.New(t)
	hooks, _ := decisionHooks(api.DecisionOnce)
	bt := testBrowserTool(t, hooks, nil)
	bt.launch = func(ctx context.Context, o browser.Options) (*browser.Browser, error) {
		return browser.Attach(ctx, page.URL, o)
	}
	bt.dir = filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(bt.dir, nil, 0o600))
	out := bt.run(context.Background(), BrowserInput{Action: "record"})
	assert.NotEmpty(t, out.Error)
	assert.Empty(t, bt.rec, "not recording")
}

// Closing a browser tool that was never made does nothing.
func TestBrowserToolCloseNil(t *testing.T) {
	var bt *browserTool
	assert.NotPanics(t, bt.close)
}

// An idle timer that fired as an action began (too late to stop) leaves
// the browser the action is using.
func TestBrowserToolIdleTimerLostTheRace(t *testing.T) {
	page := browsertest.New(t)
	hooks, _ := decisionHooks(api.DecisionOnce)
	bt := testBrowserTool(t, hooks, nil)
	bt.launch = func(ctx context.Context, o browser.Options) (*browser.Browser, error) {
		return browser.Attach(ctx, page.URL, o)
	}
	orig := browserIdle
	t.Cleanup(func() { browserIdle = orig })
	ctx := context.Background()

	browserIdle = time.Millisecond
	bt.mu.Lock()
	_, err := bt.browser(ctx)
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond) // the timer fires and waits for the lock
	browserIdle = time.Hour
	b, err := bt.browser(ctx) // the next action
	require.NoError(t, err)
	bt.mu.Unlock()
	time.Sleep(50 * time.Millisecond) // the stale timer runs

	bt.mu.Lock()
	defer bt.mu.Unlock()
	assert.Same(t, b, bt.b, "the browser in use was closed")
}

// A walkthrough frame that can't be saved is noted on the step and when
// the recording stops.
func TestBrowserToolFrameNotSaved(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	page := browsertest.New(t)
	hooks, _ := decisionHooks(api.DecisionOnce)
	bt := testBrowserTool(t, hooks, nil)
	bt.launch = func(ctx context.Context, o browser.Options) (*browser.Browser, error) {
		return browser.Attach(ctx, page.URL, o)
	}
	ctx := context.Background()
	out := bt.run(ctx, BrowserInput{Action: "record"})
	require.Empty(t, out.Error)
	require.NoError(t, os.Chmod(out.Recording, 0o500))
	t.Cleanup(func() { os.Chmod(out.Recording, 0o700) })

	out = bt.run(ctx, BrowserInput{Action: "navigate", URL: "https://shop.example/"})
	require.Empty(t, out.Error)
	assert.Contains(t, out.Note, "screenshot wasn't saved")
	out = bt.run(ctx, BrowserInput{Action: "stop_recording"})
	require.Empty(t, out.Error)
	assert.Contains(t, out.Note, "incomplete")
}
