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

// appearance is the window's appearance: the system's, light or dark.
type appearance int

const (
	appearanceSystem appearance = iota
	appearanceLight
	appearanceDark
)

// setAppearance sets the window's appearance (on macOS, the translucent
// window's blur); a variable so tests can watch it.
var setAppearance = nativeAppearance

// SetAppearance gives the window the theme the user chose ("light" or
// "dark"), not the system's: on macOS the translucent window's blur
// otherwise follows the system, so a light page in a dark system sat on a
// dark blur. "system" follows the system again; it must, since the page's
// prefers-color-scheme reports the window's appearance, and a window held
// light would hide the system's switching to dark. Anything else is
// ignored.
func (a *App) SetAppearance(theme string) {
	switch theme {
	case "system":
		setAppearance(appearanceSystem)
	case "light":
		setAppearance(appearanceLight)
	case "dark":
		setAppearance(appearanceDark)
	}
}
