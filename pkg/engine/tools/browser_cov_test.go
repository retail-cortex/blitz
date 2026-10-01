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
