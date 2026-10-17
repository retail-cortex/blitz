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
	"sync"

	"fyne.io/systray"
	"github.com/retail-cortex/blitz/apps/tray/internal/tray"
)

// submenu draws entries (tray.MCPEntries, tray.WorkerEntries) under a
// menu item, again only when they change: the old items go, the new ones
// come, each click calling click with its action.
type submenu struct {
	parent *systray.MenuItem
	mu     sync.Mutex
	key    string
	items  []*systray.MenuItem
	done   chan struct{} // closed when the items go, ending their clicks' goroutines
}

// show draws entries, unless they're what's drawn.
func (m *submenu) show(entries []tray.Entry, click func(tray.Action)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := tray.Key(entries)
	if k == m.key {
		return
	}
	m.key = k
	if m.done != nil {
		close(m.done)
	}
	for i := len(m.items) - 1; i >= 0; i-- { // a submenu's items before it
		m.items[i].Remove()
	}
	m.items, m.done = nil, make(chan struct{})
	m.add(m.parent, entries, click)
}

// add draws entries under parent, and their own under each.
func (m *submenu) add(parent *systray.MenuItem, entries []tray.Entry, click func(tray.Action)) {
	for _, e := range entries {
		var mi *systray.MenuItem
		if e.Checkbox {
			mi = parent.AddSubMenuItemCheckbox(e.Title, "", e.Checked)
		} else {
			mi = parent.AddSubMenuItem(e.Title, "")
		}
		if e.Disabled {
			mi.Disable()
		}
		m.items = append(m.items, mi)
		if len(e.Children) > 0 {
			m.add(mi, e.Children, click)
			continue
		}
		if e.Action.Kind == tray.NoAction {
			continue
		}
		go func(clicked chan struct{}, done chan struct{}, a tray.Action) {
			for {
				select {
				case <-clicked:
					click(a)
				case <-done:
					return
				}
			}
		}(mi.ClickedCh, m.done, e.Action)
	}
}
