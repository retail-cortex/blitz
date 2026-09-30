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

package browser_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/engine/browser"
	"github.com/retail-cortex/blitz/pkg/engine/browser/browsertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The page actions, against a fake DevTools page.
func TestBrowserActions(t *testing.T) {
	page := browsertest.New(t)
	ctx := context.Background()
	decided := make(chan string, 4)
	b, err := browser.Attach(ctx, page.URL, browser.Options{Timeout: 2 * time.Second, Navigation: func(u *url.URL) error {
		decided <- u.Host
		return errors.New("not approved")
	}})
	require.NoError(t, err)
	defer b.Close()

	p, err := b.Navigate(ctx, "https://shop.example/")
	require.NoError(t, err)
	assert.Equal(t, browser.Page{URL: "https://shop.example/", Title: browsertest.Title}, p)
	assert.Equal(t, []browser.ConsoleLine{{Level: "log", Text: "loaded 1"}}, b.Console())
	assert.Empty(t, b.Console(), "each message once")

	text, cut, err := b.Text(ctx, "", 20)
	require.NoError(t, err)
	assert.True(t, cut)
	assert.Equal(t, "Hello from the fake …", text)
	html, _, err := b.HTML(ctx, "h1", 0)
	require.NoError(t, err)
	assert.Equal(t, "<h1>Fake</h1>", html)

	for _, tt := range []struct {
		name    string
		run     func() error
		wantErr string
	}{
		{"click", func() error { return b.Click(ctx, "text=Buy") }, ""},
		{"click nothing", func() error { return b.Click(ctx, "#missing") }, `no element matches "#missing"`},
		{"type", func() error { return b.Type(ctx, "#q", "socks", false, true) }, ""},
		{"type appending", func() error { return b.Type(ctx, "#q", "!", true, false) }, ""},
		{"type nowhere", func() error { return b.Type(ctx, "#missing", "x", false, false) }, "no element"},
		{"select", func() error { return b.Select(ctx, "#size", "Large") }, ""},
		{"select no option", func() error { return b.Select(ctx, "#size", "Huge") }, `has no option "Huge"`},
		{"select nothing", func() error { return b.Select(ctx, "#missing", "x") }, "no element"},
		{"read nothing", func() error { _, _, err := b.Text(ctx, "#missing", 0); return err }, "no element"},
		{"script error", func() error { _, err := b.Eval(ctx, "throw new Error('boom')"); return err }, "Error: boom"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run()
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}

	// The click went to another site: the page's own navigation was put
	// to Navigation, and refused.
	select {
	case host := <-decided:
		assert.Equal(t, "away.example", host)
	case <-time.After(5 * time.Second):
		t.Fatal("the page's navigation wasn't decided")
	}
	require.Eventually(t, func() bool { return page.Decided()["https://away.example/"] == "fail" }, 5*time.Second, 10*time.Millisecond)
	assert.Equal(t, "continue", page.Decided()["https://shop.example/"], "the navigation asked for goes ahead")
	assert.Contains(t, fmt.Sprint(b.Refused()), "away.example: not approved")
	assert.Empty(t, b.Refused())

	v, err := b.Eval(ctx, "1+1")
	require.NoError(t, err)
	assert.Equal(t, "2", v)
	v, err = b.Eval(ctx, "undefined")
	require.NoError(t, err)
	assert.Equal(t, "", v)

	p, err = b.Back(ctx)
	require.NoError(t, err)
	assert.Equal(t, "https://back.example/", p.URL)

	png, err := b.Screenshot(ctx, true)
	require.NoError(t, err)
	assert.Equal(t, browsertest.PNG, png)

	require.NoError(t, b.Close())
	_, err = b.Page(ctx)
	assert.Error(t, err, "closed")
	assert.Contains(t, page.Methods(), "Input.insertText")
	assert.Contains(t, page.Methods(), "Input.dispatchKeyEvent")
}

func TestAttachFails(t *testing.T) {
	_, err := browser.Attach(context.Background(), "ws://127.0.0.1:1/devtools", browser.Options{})
	assert.Error(t, err)
}
