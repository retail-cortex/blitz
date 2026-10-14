// Copyright 2026 Retail Cortex
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package main

import (
	goruntime "runtime"

	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// The macOS menu bar (DSK-101): the app menu as a Mac app has it, About
// Blitz and Settings… (⌘,) included, then Wails's Edit and Window menus.
// About and Settings open the page's own (Settings › About, Settings ›
// Appearance) through the events menu:about and menu:settings. Elsewhere
// the page's menu bar is the only one.

// MenuLabels are the app menu's words in the window's language: the page
// sends them (SetMenu) as it starts and when its language changes; until
// then they're English.
type MenuLabels struct {
	About      string `json:"about"`
	Settings   string `json:"settings"`
	Hide       string `json:"hide"`
	HideOthers string `json:"hide_others"`
	ShowAll    string `json:"show_all"`
	Quit       string `json:"quit"`
}

var englishMenu = MenuLabels{About: "About Blitz", Settings: "Settings…", Hide: "Hide Blitz", HideOthers: "Hide Others", ShowAll: "Show All", Quit: "Quit Blitz"}

// withDefaults is l with English for what it leaves out.
func (l MenuLabels) withDefaults() MenuLabels {
	for _, f := range []struct {
		got *string
		def string
	}{
		{&l.About, englishMenu.About}, {&l.Settings, englishMenu.Settings}, {&l.Hide, englishMenu.Hide},
		{&l.HideOthers, englishMenu.HideOthers}, {&l.ShowAll, englishMenu.ShowAll}, {&l.Quit, englishMenu.Quit},
	} {
		if *f.got == "" {
			*f.got = f.def
		}
	}
	return l
}

// appMenu is the menu bar with labels l: nil off macOS.
func (a *App) appMenu(l MenuLabels) *menu.Menu {
	if goruntime.GOOS != "darwin" {
		return nil
	}
	l = l.withDefaults()
	m := menu.NewMenu()
	app := m.AddSubmenu("Blitz") // macOS names the first menu after the app
	app.AddText(l.About, nil, a.pageEvent("menu:about"))
	app.AddSeparator()
	app.AddText(l.Settings, keys.CmdOrCtrl(","), a.pageEvent("menu:settings"))
	app.AddSeparator()
	app.AddText(l.Hide, keys.CmdOrCtrl("h"), func(*menu.CallbackData) { runtime.Hide(a.ctx) })
	app.AddText(l.HideOthers, keys.Combo("h", keys.CmdOrCtrlKey, keys.OptionOrAltKey), func(*menu.CallbackData) { hideOtherApps() })
	app.AddText(l.ShowAll, nil, func(*menu.CallbackData) { showAllApps() })
	app.AddSeparator()
	// Quit asks first about unsaved changes, as closing the window does
	// (Wails runs OnBeforeClose).
	app.AddText(l.Quit, keys.CmdOrCtrl("q"), func(*menu.CallbackData) { runtime.Quit(a.ctx) })
	m.Append(menu.EditMenu())
	m.Append(menu.WindowMenu())
	return m
}

// pageEvent is a menu item's action that tells the page name.
func (a *App) pageEvent(name string) menu.Callback {
	return func(*menu.CallbackData) {
		if a.emit != nil {
			a.emit(name)
		} else if a.ctx != nil {
			runtime.EventsEmit(a.ctx, name)
		}
	}
}

// SetMenu relabels the app menu in the page's language (macOS; nothing
// elsewhere).
func (a *App) SetMenu(l MenuLabels) {
	m := a.appMenu(l)
	if m == nil || a.ctx == nil {
		return
	}
	runtime.MenuSetApplicationMenu(a.ctx, m)
	runtime.MenuUpdateApplicationMenu(a.ctx)
}
