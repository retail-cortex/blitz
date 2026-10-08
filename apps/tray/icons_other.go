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

//go:build !windows

package main

import (
	_ "embed"

	"fyne.io/systray"
)

// The icons: a white bolt, filled while the service runs and outlined
// while it's stopped (icons/*.svg). macOS takes them as templates and
// tints them to suit the menu bar; Linux shows them as they are.
var (
	//go:embed icons/running-44.png
	runningIcon []byte
	//go:embed icons/stopped-44.png
	stoppedIcon []byte
)

// setIcon shows the service's state in the icon.
func setIcon(running bool) {
	if running {
		systray.SetTemplateIcon(runningIcon, runningIcon)
	} else {
		systray.SetTemplateIcon(stoppedIcon, stoppedIcon)
	}
}
