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
	goruntime "runtime"

	"fyne.io/systray"
	"github.com/retail-cortex/blitz/apps/tray/internal/tray"
)

// The icons: on macOS a white bolt, filled while the service runs and
// outlined while it's stopped (icons/*.svg), which macOS takes as
// templates and tints to suit the menu bar. Linux shows icons as they
// are, on light panels and dark, so there the bolt is in colour as on
// Windows: amber while the service runs, grey while it's stopped
// (icons/windows-*.svg).
var (
	//go:embed icons/running-44.png
	runningIcon []byte
	//go:embed icons/stopped-44.png
	stoppedIcon []byte
	//go:embed icons/windows-running-64.png
	runningColour []byte
	//go:embed icons/windows-stopped-64.png
	stoppedColour []byte
)

// setIcon shows the service's state in the icon, and a dot while
// workflows' human tasks wait (waiting).
func setIcon(running, waiting bool) {
	template, colour := stoppedIcon, stoppedColour
	if running {
		template, colour = runningIcon, runningColour
	}
	if waiting {
		template, colour = dotted(template), dotted(colour)
	}
	if goruntime.GOOS == "darwin" {
		systray.SetTemplateIcon(template, template)
		return
	}
	systray.SetIcon(colour)
}

// dotted is an icon with the waiting tasks' dot (the icon itself if that fails).
func dotted(icon []byte) []byte {
	if d, err := tray.WithDot(icon); err == nil {
		return d
	}
	return icon
}
