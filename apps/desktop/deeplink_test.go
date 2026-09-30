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

package main

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDeepLink(t *testing.T) {
	dir := t.TempDir()
	q := url.QueryEscape
	tests := []struct {
		name, link string
		want       DeepLink
		err        string
	}{
		{name: "a folder and a prompt", link: "blitz://open?dir=" + q(dir) + "&prompt=" + q("fix the build"), want: DeepLink{Dir: dir, Prompt: "fix the build"}},
		{name: "without //", link: "blitz:open?dir=" + q(dir), want: DeepLink{Dir: dir}},
		{name: "another scheme", link: "https://open?dir=" + q(dir), err: "not a blitz:// link"},
		{name: "another action", link: "blitz://run?dir=" + q(dir), err: "unknown link action"},
		{name: "a relative folder", link: "blitz://open?dir=src", err: "absolute dir="},
		{name: "no folder", link: "blitz://open?prompt=hi", err: "absolute dir="},
		{name: "a missing folder", link: "blitz://open?dir=" + q(dir+"/nope"), err: "isn't a folder"},
		{name: "a long prompt", link: "blitz://open?dir=" + q(dir) + "&prompt=" + strings.Repeat("a", maxLinkPrompt+1), err: "longer than"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDeepLink(tt.link)
			if tt.err != "" {
				assert.ErrorContains(t, err, tt.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// Links wait for the page, then go to it as they come; bad ones are
// dropped.
func TestLinksQueue(t *testing.T) {
	dir := t.TempDir()
	var shown []DeepLink
	a := &App{ctx: context.Background()}
	a.links.emit = func(_ context.Context, l DeepLink) { shown = append(shown, l) }

	a.links.receive("blitz://open?dir=" + url.QueryEscape(dir) + "&prompt=first")
	a.links.receive("blitz://nonsense")
	assert.Empty(t, shown, "not before the page listens")
	assert.Equal(t, []DeepLink{{Dir: dir, Prompt: "first"}}, a.PendingLinks())
	assert.Empty(t, a.PendingLinks(), "once")

	a.links.receive("blitz://open?dir=" + url.QueryEscape(dir) + "&prompt=second")
	assert.Equal(t, []DeepLink{{Dir: dir, Prompt: "second"}}, shown)
	assert.Equal(t, []string{"blitz://open?dir=x"}, linkArgs([]string{"-v", "blitz://open?dir=x", "other"}))
}
