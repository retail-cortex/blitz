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
	"time"

	"github.com/retail-cortex/blitz/apps/tray/internal/tray"
)

// pulseEvery is how long each of the pulse's frames shows.
const pulseEvery = 250 * time.Millisecond

// trayIcon is the tray's icon: the service's state, a dot while tasks
// wait, and, while workflows or jobs run, a slow pulse (spec_electron_038
// EL-111) through tray.PulseFrames.
type trayIcon struct {
	mu                     sync.Mutex
	running, waiting, busy bool
	frame                  int
}

func newTrayIcon() *trayIcon {
	t := &trayIcon{}
	go t.loop()
	return t
}

// set shows the service's state and the tasks' dot (the pulse goes on
// drawing it while something runs).
func (t *trayIcon) set(running, waiting bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.running, t.waiting = running, waiting
	if !t.busy || !running {
		setIcon(running, waiting, 1)
	}
}

// pulse starts or stops the pulse: busy while anything runs.
func (t *trayIcon) pulse(busy bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.busy && !busy {
		t.frame = 0
		setIcon(t.running, t.waiting, 1)
	}
	t.busy = busy
}

func (t *trayIcon) loop() {
	for range time.Tick(pulseEvery) {
		t.mu.Lock()
		if t.busy && t.running {
			t.frame = (t.frame + 1) % len(tray.PulseFrames)
			setIcon(t.running, t.waiting, tray.PulseFrames[t.frame])
		}
		t.mu.Unlock()
	}
}
