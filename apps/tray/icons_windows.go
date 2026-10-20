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
	_ "embed"
	"sync"

	"fyne.io/systray"
	"github.com/retail-cortex/blitz/apps/tray/internal/tray"
)

// The icons on Windows: the bolt in colour, amber while the service runs
// and grey while it's stopped (icons/windows-*.svg). Windows shows them as
// they are, on light and dark taskbars, and wants them as .ico.
var (
	//go:embed icons/windows-running-64.png
	runningPNG []byte
	//go:embed icons/windows-stopped-64.png
	stoppedPNG []byte

	runningIcon = mustICO(runningPNG)
	stoppedIcon = mustICO(stoppedPNG)
)

func mustICO(p []byte) []byte {
	ico, err := tray.ICO(p)
	if err != nil {
		panic(err) // the embedded PNGs are tested
	}
	return ico
}

// setIcon shows the service's state in the icon, a dot while workflows'
// human tasks wait (waiting), at a brightness (alpha: the pulse's frame).
func setIcon(running, waiting bool, alpha float64) {
	if !waiting && alpha >= 1 {
		systray.SetIcon(map[bool][]byte{true: runningIcon, false: stoppedIcon}[running])
		return
	}
	k := frameKey{running, waiting, alpha}
	framesMu.Lock()
	defer framesMu.Unlock()
	if ico, ok := frames[k]; ok {
		systray.SetIcon(ico)
		return
	}
	p := map[bool][]byte{true: runningPNG, false: stoppedPNG}[running]
	var err error
	if waiting {
		p, err = tray.WithDot(p)
	}
	if err == nil && alpha < 1 {
		p, err = tray.Pulse(p, alpha)
	}
	ico := map[bool][]byte{true: runningIcon, false: stoppedIcon}[running]
	if err == nil {
		if made, e := tray.ICO(p); e == nil {
			ico = made
		}
	}
	frames[k] = ico
	systray.SetIcon(ico)
}

var (
	framesMu sync.Mutex
	frames   = map[frameKey][]byte{}
)

type frameKey struct {
	running, waiting bool
	alpha            float64
}
