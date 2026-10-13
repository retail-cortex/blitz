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
	goruntime "runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wailsapp/wails/v2/pkg/menu"
)

// Labels left out are English; those given are kept.
func TestMenuLabelsDefaults(t *testing.T) {
	assert.Equal(t, englishMenu, MenuLabels{}.withDefaults())
	got := MenuLabels{About: "À propos de Blitz", Quit: "Quitter Blitz"}.withDefaults()
	assert.Equal(t, "À propos de Blitz", got.About)
	assert.Equal(t, "Quitter Blitz", got.Quit)
	assert.Equal(t, englishMenu.Settings, got.Settings)
}

// macOS's app menu: About, Settings… (⌘,), Hide, Hide Others, Show All
// and Quit, then Edit and Window; About and Settings tell the page.
func TestAppMenu(t *testing.T) {
	var told []string
	a := &App{emit: func(name string) { told = append(told, name) }}
	m := a.appMenu(MenuLabels{Settings: "Réglages…"})
	if goruntime.GOOS != "darwin" {
		assert.Nil(t, m, "the page's menu bar is the only one")
		a.SetMenu(MenuLabels{}) // nothing to relabel
		return
	}
	require.NotNil(t, m)
	require.Len(t, m.Items, 3)
	assert.Equal(t, menu.Role(menu.EditMenuRole), m.Items[1].Role)
	assert.Equal(t, menu.Role(menu.WindowMenuRole), m.Items[2].Role)
	var labels []string
	byLabel := map[string]*menu.MenuItem{}
	for _, it := range m.Items[0].SubMenu.Items {
		if it.Type == menu.SeparatorType {
			labels = append(labels, "—")
			continue
		}
		labels = append(labels, it.Label)
		byLabel[it.Label] = it
	}
	assert.Equal(t, []string{"About Blitz", "—", "Réglages…", "—", "Hide Blitz", "Hide Others", "Show All", "—", "Quit Blitz"}, labels)
	require.NotNil(t, byLabel["Réglages…"].Accelerator)
	assert.Equal(t, ",", byLabel["Réglages…"].Accelerator.Key)
	assert.Equal(t, "q", byLabel["Quit Blitz"].Accelerator.Key)
	byLabel["About Blitz"].Click(nil)
	byLabel["Réglages…"].Click(nil)
	assert.Equal(t, []string{"menu:about", "menu:settings"}, told)
	a.SetMenu(MenuLabels{}) // no window yet: nothing to relabel
}
