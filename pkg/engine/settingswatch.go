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
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
)

// The engine keeps its permission rules in step with the files they come
// from, whoever changes them: the desktop app, a terminal's /permissions
// --save, an editor, another process. It looks at the global and the
// workspace's settings, the project's files and the approvals file every
// settingsPoll, and applies a change to the live rules, which every tool
// call reads, so a turn already running obeys it from its next action.
// Listeners (the service, for its clients) hear of it.

// settingsPoll is how often the settings files are looked at.
var settingsPoll = 2 * time.Second

// fileStamp is what a look at a file sees.
type fileStamp struct {
	mod    time.Time
	size   int64
	exists bool
}

func stampOf(path string) fileStamp {
	info, err := os.Stat(path)
	if err != nil {
		return fileStamp{}
	}
	return fileStamp{mod: info.ModTime(), size: info.Size(), exists: true}
}

// settingsFiles are the files the rules come from, and the approvals file.
func (w *Workspace) settingsFiles() (rules []string, approvals string) {
	prefix := w.cfg.Dir
	rules = []string{
		filepath.Join(config.ConfigDir(prefix), ".env.toml"),
		filepath.Join(config.WorkspaceSettingsDir(prefix, w.Dir()), ".env.toml"),
	}
	for _, f := range config.ProjectFiles {
		rules = append(rules, filepath.Join(w.Dir(), filepath.FromSlash(f)))
	}
	return rules, w.tools.Hooks().Store().Path()
}

// settingsWatch looks at the settings files until stopped.
type settingsWatch struct {
	stop chan struct{}
	done chan struct{}
	once sync.Once
}

// OnSettingsChanged calls f after the rules or approvals changed on disk
// and were applied, until the returned function is called.
func (w *Workspace) OnSettingsChanged(f func()) (stop func()) {
	w.settingsMu.Lock()
	defer w.settingsMu.Unlock()
	if w.settingsListeners == nil {
		w.settingsListeners = map[int]func(){}
	}
	w.settingsNext++
	id := w.settingsNext
	w.settingsListeners[id] = f
	return func() {
		w.settingsMu.Lock()
		defer w.settingsMu.Unlock()
		delete(w.settingsListeners, id)
	}
}

// SettingsChanges is a channel that hears of settings changes (at most
// one pending) until ctx ends.
func (w *Workspace) SettingsChanges(ctx context.Context) <-chan struct{} {
	ch := make(chan struct{}, 1)
	stop := w.OnSettingsChanged(func() {
		select {
		case ch <- struct{}{}:
		default:
		}
	})
	context.AfterFunc(ctx, stop)
	return ch
}

// watchSettings starts looking at the settings files.
func (w *Workspace) watchSettings() *settingsWatch {
	sw := &settingsWatch{stop: make(chan struct{}), done: make(chan struct{})}
	rules, approvals := w.settingsFiles()
	seen := map[string]fileStamp{}
	for _, f := range append(rules, approvals) {
		seen[f] = stampOf(f)
	}
	go func() {
		defer close(sw.done)
		t := time.NewTicker(settingsPoll)
		defer t.Stop()
		for {
			select {
			case <-sw.stop:
				return
			case <-t.C:
			}
			rulesChanged, approvalsChanged := false, false
			for _, f := range rules {
				if s := stampOf(f); s != seen[f] {
					seen[f], rulesChanged = s, true
				}
			}
			if approvals != "" {
				if s := stampOf(approvals); s != seen[approvals] {
					seen[approvals], approvalsChanged = s, true
				}
			}
			if rulesChanged {
				w.reloadRules()
			}
			if approvalsChanged {
				if err := w.tools.Hooks().Store().Reload(); err != nil {
					w.warn("approvals: " + err.Error())
				}
			}
			if rulesChanged || approvalsChanged {
				w.settingsMu.Lock()
				listeners := slices.Collect(maps.Values(w.settingsListeners))
				w.settingsMu.Unlock()
				for _, f := range listeners {
					f()
				}
			}
		}
	}()
	return sw
}

// close stops the watch and waits for it.
func (sw *settingsWatch) close() {
	if sw == nil {
		return
	}
	sw.once.Do(func() { close(sw.stop) })
	<-sw.done
}

// reloadRules applies the settings as they are now on disk.
func (w *Workspace) reloadRules() {
	cfg, err := config.LoadWorkspace(w.cfg.Dir, w.Dir())
	if err != nil {
		w.warn("reloading the permission rules: " + err.Error())
		return
	}
	if err := w.ReloadPermissions(cfg); err != nil {
		w.warn("reloading the permission rules: " + err.Error())
	}
}
