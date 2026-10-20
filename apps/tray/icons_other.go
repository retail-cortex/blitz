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
	"sync"

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

// setIcon shows the service's state in the icon, a dot while workflows'
// human tasks wait (waiting), at a brightness (alpha: the pulse's frame).
func setIcon(running, waiting bool, alpha float64) {
	template, colour := stoppedIcon, stoppedColour
	if running {
		template, colour = runningIcon, runningColour
	}
	if goruntime.GOOS == "darwin" {
		template = iconFrame(template, waiting, alpha)
		systray.SetTemplateIcon(template, template)
		return
	}
	systray.SetIcon(iconFrame(colour, waiting, alpha))
}

var (
	framesMu sync.Mutex
	frames   = map[frameKey][]byte{}
)

type frameKey struct {
	icon    *byte
	waiting bool
	alpha   float64
}

// iconFrame is icon with the tasks' dot (waiting) at a brightness, made
// once (the icon itself if that fails).
func iconFrame(icon []byte, waiting bool, alpha float64) []byte {
	k := frameKey{&icon[0], waiting, alpha}
	framesMu.Lock()
	defer framesMu.Unlock()
	if f, ok := frames[k]; ok {
		return f
	}
	f := icon
	if waiting {
		f = dotted(f)
	}
	if alpha < 1 {
		if p, err := tray.Pulse(f, alpha); err == nil {
			f = p
		}
	}
	frames[k] = f
	return f
}

// dotted is an icon with the waiting tasks' dot (the icon itself if that fails).
func dotted(icon []byte) []byte {
	if d, err := tray.WithDot(icon); err == nil {
		return d
	}
	return icon
}
