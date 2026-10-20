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
	"embed"
	goruntime "runtime"
	"strings"
	"sync"

	"fyne.io/systray"
	"github.com/retail-cortex/blitz/apps/tray/internal/tray"
)

// The menu's icons (icons/menu, the app's Material icons; spec_electron_038
// EL-111): on macOS the black template of each, which macOS draws in the
// menu's colour; elsewhere the grey one (as .ico on Windows). The dots
// keep their colours everywhere.
//
//go:embed icons/menu/*.png
var menuIconFiles embed.FS

var (
	menuIconMu    sync.Mutex
	menuIconCache = map[string][]byte{}
)

// menuIcon is the icon called name, as this system wants it (nil when
// there's none by that name).
func menuIcon(name string) []byte {
	menuIconMu.Lock()
	defer menuIconMu.Unlock()
	if b, ok := menuIconCache[name]; ok {
		return b
	}
	file := name + "-grey.png"
	switch {
	case strings.HasPrefix(name, "dot-"):
		file = name + ".png"
	case goruntime.GOOS == "darwin":
		file = name + "-template.png"
	}
	b, err := menuIconFiles.ReadFile("icons/menu/" + file)
	if err == nil && goruntime.GOOS == "windows" {
		b, err = tray.ICO(b)
	}
	if err != nil {
		b = nil
	}
	menuIconCache[name] = b
	return b
}

// setItemIcon puts the icon called name on an item ("" or an unknown
// name: none is set).
func setItemIcon(mi *systray.MenuItem, name string) {
	b := menuIcon(name)
	if b == nil {
		return
	}
	if goruntime.GOOS == "darwin" && !strings.HasPrefix(name, "dot-") {
		mi.SetTemplateIcon(b, b)
		return
	}
	mi.SetIcon(b)
}
