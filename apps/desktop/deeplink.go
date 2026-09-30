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
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Deep links (spec_parity_027 PAR-INT-03): blitz://open?dir=<folder>&
// prompt=<text> opens the folder as a workspace with the prompt in its
// composer, never sent without the person. macOS hands links over through
// the app's URL scheme (OnUrlOpen), Linux as an argument, to this process
// or, through the single-instance lock, to the one already running.

// DeepLink is a link's workspace and prompt.
type DeepLink struct {
	Dir    string `json:"dir"`
	Prompt string `json:"prompt"`
}

// maxLinkPrompt bounds a link's prompt.
const maxLinkPrompt = 20000

// parseDeepLink reads blitz://open?dir=…&prompt=….
func parseDeepLink(raw string) (DeepLink, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "blitz" {
		return DeepLink{}, fmt.Errorf("not a blitz:// link: %q", raw)
	}
	if action := cmpOr(u.Host, strings.TrimPrefix(u.Opaque, "//")); strings.TrimSuffix(action, "/") != "open" && !strings.HasPrefix(u.Opaque, "open") {
		return DeepLink{}, fmt.Errorf("unknown link action in %q (blitz://open?dir=…&prompt=…)", raw)
	}
	q := u.Query() // blitz:open?… too: the query isn't part of the opaque
	dir := config.ExpandHome(q.Get("dir"))
	if dir == "" || !filepath.IsAbs(dir) {
		return DeepLink{}, errors.New("a link names its folder with an absolute dir=")
	}
	dir = filepath.Clean(dir)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return DeepLink{}, fmt.Errorf("%s isn't a folder", dir)
	}
	prompt := q.Get("prompt")
	if utf8.RuneCountInString(prompt) > maxLinkPrompt {
		return DeepLink{}, fmt.Errorf("the link's prompt is longer than %d characters", maxLinkPrompt)
	}
	return DeepLink{Dir: dir, Prompt: prompt}, nil
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// links hands links to the page: at once when it's up, else when it asks.
type links struct {
	mu      sync.Mutex
	ctx     context.Context // the window's, once started
	pending []DeepLink
	emit    func(ctx context.Context, link DeepLink) // runtime's, replaced in tests
}

// receive takes a link from the system.
func (l *links) receive(raw string) {
	link, err := parseDeepLink(raw)
	if err != nil {
		log.Printf("deep link: %v", err)
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.ctx == nil {
		l.pending = append(l.pending, link)
		return
	}
	l.emit(l.ctx, link)
}

// started is when the page listens.
func (l *links) started(ctx context.Context) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ctx = ctx
}

// take is the links that came before the page listened.
func (l *links) take() []DeepLink {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := l.pending
	l.pending = nil
	return out
}

// showLink brings the window forward and tells the page.
func showLink(ctx context.Context, link DeepLink) {
	runtime.WindowUnminimise(ctx)
	runtime.WindowShow(ctx)
	runtime.EventsEmit(ctx, "deeplink:open", link)
}

// PendingLinks are the links that came before the page listened; from
// now on they go to it as they come (deeplink:open).
func (a *App) PendingLinks() []DeepLink {
	out := a.links.take()
	a.links.started(a.ctx)
	return out
}

// linkArgs are the blitz:// links among a command line's arguments.
func linkArgs(args []string) []string {
	var out []string
	for _, a := range args {
		if strings.HasPrefix(a, "blitz:") {
			out = append(out, a)
		}
	}
	return out
}
