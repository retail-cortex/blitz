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
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
)

// Prefs are the window's own settings, kept in ~/.blitz/desktop.json:
// how it looks, and the workspaces it knows with the names and colours
// the user gave them. Everything else belongs to the service.
type Prefs struct {
	// Theme is "system" (the default), "light" or "dark".
	Theme string `json:"theme"`
	// Workspaces are every workspace the window has opened, open ones in
	// tab order, then closed ones (most recent first) for reopening.
	Workspaces []WorkspacePrefs `json:"workspaces"`
	// Active is the directory of the workspace in view.
	Active string `json:"active,omitempty"`
	// RunSettings shows the run settings panel beside the conversation.
	RunSettings bool `json:"run_settings"`
	// ShowThoughts shows the model's thinking, collapsed.
	ShowThoughts bool `json:"show_thoughts"`
	// Density is "comfortable" (the default) or "compact".
	Density string `json:"density"`
	// Notifications is "on" (the default: when the agent finishes or waits
	// while the window is elsewhere) or "off".
	Notifications string `json:"notifications"`
	// Language is the window's language: "system" (the default) or a
	// catalog's tag such as "fr-CA".
	Language string `json:"language"`
	// Files shows the Files shelf beside the conversation.
	Files bool `json:"files"`
	// ShowHidden lists hidden files in the shelf: dotfiles, files git
	// ignores, and the agent's blocked paths.
	ShowHidden bool `json:"show_hidden"`
	// ChatWidth is the chat panel's width in pixels (0: a third of the
	// window).
	ChatWidth int `json:"chat_width,omitempty"`
	// Width is how wide the conversation runs: "full" (the default, the
	// whole panel) or "readable" (a column of about 860 px).
	Width string `json:"width"`
}

// WorkspacePrefs is a workspace as the window shows it.
type WorkspacePrefs struct {
	Dir         string `json:"dir"`
	Name        string `json:"name,omitempty"` // "" shows the directory's name
	Description string `json:"description,omitempty"`
	Color       string `json:"color,omitempty"` // one of the palette's names
	Open        bool   `json:"open"`
}

// languageTag is what the language setting may hold.
var languageTag = regexp.MustCompile(`^(system|[A-Za-z]{2,3}(-[A-Za-z0-9]+)*)$`)

// maxRecent bounds the closed workspaces remembered.
const maxRecent = 20

var themes = map[string]bool{"system": true, "light": true, "dark": true}

// normalize fills in defaults and drops what can't be right: unknown
// themes, duplicate or relative directories, and old closed workspaces.
func (p *Prefs) normalize() {
	if !themes[p.Theme] {
		p.Theme = "system"
	}
	if p.Density != "compact" {
		p.Density = "comfortable"
	}
	if p.Width != "readable" {
		p.Width = "full"
	}
	if p.Notifications != "off" {
		p.Notifications = "on"
	}
	if !languageTag.MatchString(p.Language) {
		p.Language = "system"
	}
	if p.ChatWidth < 0 || p.ChatWidth > 10000 {
		p.ChatWidth = 0
	}
	seen := map[string]bool{}
	var open, closed []WorkspacePrefs
	for _, w := range p.Workspaces {
		w.Dir = filepath.Clean(w.Dir)
		if !filepath.IsAbs(w.Dir) || seen[w.Dir] {
			continue
		}
		seen[w.Dir] = true
		w.Name = strings.TrimSpace(w.Name)
		w.Description = strings.TrimSpace(w.Description)
		if w.Open {
			open = append(open, w)
		} else if len(closed) < maxRecent {
			closed = append(closed, w)
		}
	}
	// Never nil: the page reads a list (JSON null would break it).
	p.Workspaces = append(append([]WorkspacePrefs{}, open...), closed...)
	if !slices.ContainsFunc(open, func(w WorkspacePrefs) bool { return w.Dir == p.Active }) {
		p.Active = ""
	}
	if p.Active == "" && len(open) > 0 {
		p.Active = open[0].Dir
	}
}

// prefsStore reads and writes the preferences file.
type prefsStore struct {
	mu   sync.Mutex
	path string
}

// load returns the saved preferences, or the defaults when there are none.
// An unreadable file is set aside (desktop.json.damaged) rather than lost.
func (s *prefsStore) load() (Prefs, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var p Prefs
	data, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		err = nil // nothing saved yet
	case err != nil:
		return defaults(), err
	default:
		if jerr := json.Unmarshal(data, &p); jerr != nil {
			os.Rename(s.path, s.path+".damaged")
			p = Prefs{}
			err = fmt.Errorf("the desktop settings were unreadable and were set aside: %w", jerr)
		}
	}
	p.normalize()
	return p, err
}

func defaults() Prefs {
	var p Prefs
	p.normalize()
	return p
}

// save writes the preferences atomically, owner-only.
func (s *prefsStore) save(p Prefs) (Prefs, error) {
	p.normalize()
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return p, err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return p, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".desktop-*.json")
	if err != nil {
		return p, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return p, err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return p, err
	}
	if err := tmp.Close(); err != nil {
		return p, err
	}
	return p, os.Rename(tmp.Name(), s.path)
}

// safeURL reports whether a link from model output may be opened in the
// system browser: web and mail links only, never file: or custom schemes.
func safeURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return u.Host != ""
	case "mailto":
		return u.Opaque != "" || u.Path != ""
	}
	return false
}
