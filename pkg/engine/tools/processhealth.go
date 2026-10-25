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

package tools

import (
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
)

// The background processes' health (spec_processes_052): what each uses,
// sampled while any runs, and the flags that say one looks bad.

// ProcessLimits say when a background process looks bad.
type ProcessLimits struct {
	// CPUPercent of one core, kept up for BusyFor, flags it (cpu).
	CPUPercent float64
	BusyFor    time.Duration
	// MemoryBytes, or memory up by half within GrowthWindow (and over
	// growthFloor), flags it (memory).
	MemoryBytes  int64
	GrowthWindow time.Duration
	// LeftAfter without a turn in its session flags it (left), as its
	// session's deletion does at once.
	LeftAfter time.Duration
	// SampleEvery is how often its usage is sampled.
	SampleEvery time.Duration
}

// DefaultProcessLimits are the limits when the [processes] config says
// nothing.
func DefaultProcessLimits() ProcessLimits {
	return ProcessLimits{
		CPUPercent: 90, BusyFor: 2 * time.Minute,
		MemoryBytes: 2 << 30, GrowthWindow: 10 * time.Minute,
		LeftAfter:   2 * time.Hour,
		SampleEvery: 5 * time.Second,
	}
}

// ProcessLimitsFrom reads the [processes] config; what's unset, 0 or not a
// duration keeps the default.
func ProcessLimitsFrom(c config.ProcessesConfig) ProcessLimits {
	dur := func(s string) time.Duration {
		d, err := time.ParseDuration(s)
		if err != nil {
			return 0
		}
		return d
	}
	return ProcessLimits{
		CPUPercent: c.CPUPercent, BusyFor: dur(c.BusyFor),
		MemoryBytes: int64(c.MemoryMB) << 20, GrowthWindow: dur(c.GrowthWindow),
		LeftAfter: dur(c.LeftAfter),
	}
}

// growthFloor: memory that grows below this isn't worth a flag.
const growthFloor = 256 << 20

// procHealth is what's known of a process's usage; guarded by
// ProcessManager.mu.
type procHealth struct {
	measured  bool
	lastCPU   time.Duration
	lastAt    time.Time
	cpu       float64 // percent of one core, over the last sample
	memory    int64
	busySince time.Time   // zero while under CPUPercent
	memories  []memSample // the GrowthWindow's, and the one before it
}

type memSample struct {
	at    time.Time
	bytes int64
}

// sessionActivity is when a session last had a turn; guarded by
// ProcessManager.mu.
type sessionActivity struct {
	turns int // running now
	last  time.Time
	gone  bool // deleted
}

// SetLimits sets when a process looks bad; zero fields keep the defaults.
func (m *ProcessManager) SetLimits(l ProcessLimits) {
	d := DefaultProcessLimits()
	if l.CPUPercent <= 0 {
		l.CPUPercent = d.CPUPercent
	}
	if l.BusyFor <= 0 {
		l.BusyFor = d.BusyFor
	}
	if l.MemoryBytes <= 0 {
		l.MemoryBytes = d.MemoryBytes
	}
	if l.GrowthWindow <= 0 {
		l.GrowthWindow = d.GrowthWindow
	}
	if l.LeftAfter <= 0 {
		l.LeftAfter = d.LeftAfter
	}
	if l.SampleEvery <= 0 {
		l.SampleEvery = d.SampleEvery
	}
	m.mu.Lock()
	m.limits = l
	m.mu.Unlock()
}

// TurnStarted notes a turn starting in session; the func it returns
// notes its end. A process outlives its session's turns, and is left
// running when they stop for LeftAfter.
func (m *ProcessManager) TurnStarted(session string) func() {
	if session == "" {
		return func() {}
	}
	m.mu.Lock()
	a := m.activityLocked(session)
	a.turns++
	a.last = m.now()
	m.mu.Unlock()
	return func() {
		m.mu.Lock()
		a.turns--
		a.last = m.now()
		m.mu.Unlock()
	}
}

// SessionGone notes session's deletion: its processes were left running.
func (m *ProcessManager) SessionGone(session string) {
	m.mu.Lock()
	m.activityLocked(session).gone = true
	m.mu.Unlock()
}

// activityLocked is session's activity, noted from now if new; m.mu held.
func (m *ProcessManager) activityLocked(session string) *sessionActivity {
	a, ok := m.sessions[session]
	if !ok {
		a = &sessionActivity{last: m.now()}
		m.sessions[session] = a
	}
	return a
}

// startMonitorLocked samples the processes' usage until none runs; m.mu
// held.
func (m *ProcessManager) startMonitorLocked() {
	if m.monitoring || m.sample == nil {
		return
	}
	m.monitoring = true
	every := m.limits.SampleEvery
	m.monitor.Add(1)
	go func() {
		defer m.monitor.Done()
		tick := time.NewTicker(every)
		defer tick.Stop()
		for {
			select {
			case <-m.quit: // Shutdown
				return
			case <-tick.C:
			}
			if !m.sampleOnce() {
				return
			}
		}
	}()
}

// sampleOnce samples the running processes' usage and updates their
// health; false when none runs (the monitor stops, a new process starts
// it again).
func (m *ProcessManager) sampleOnce() bool {
	m.mu.Lock()
	running := 0
	for _, p := range m.procs {
		if !p.finished {
			running++
		}
	}
	if running == 0 || m.closed {
		m.monitoring = false
		m.mu.Unlock()
		return false
	}
	sample := m.sample
	m.mu.Unlock()

	groups, err := sample()
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		return true // not sampled here: only left is flagged
	}
	for _, p := range m.procs {
		if p.finished || p.pid == 0 {
			continue
		}
		g, ok := groups[p.pid]
		if !ok {
			continue
		}
		m.updateLocked(&p.health, g, now)
	}
	return true
}

// updateLocked takes a sample into h; m.mu held.
func (m *ProcessManager) updateLocked(h *procHealth, g groupUsage, now time.Time) {
	if h.measured && now.After(h.lastAt) {
		// A child that exited takes its time with it: never below 0.
		h.cpu = max(0, float64(g.CPU-h.lastCPU)/float64(now.Sub(h.lastAt))*100)
	}
	h.measured, h.lastCPU, h.lastAt, h.memory = true, g.CPU, now, g.Memory
	switch {
	case h.cpu < m.limits.CPUPercent:
		h.busySince = time.Time{}
	case h.busySince.IsZero():
		h.busySince = now
	}
	h.memories = append(h.memories, memSample{now, g.Memory})
	start := now.Add(-m.limits.GrowthWindow)
	for len(h.memories) > 1 && !h.memories[1].at.After(start) {
		h.memories = h.memories[1:]
	}
}

// flagsLocked are why p looks bad, and how long its session has gone
// without a turn; m.mu held.
func (m *ProcessManager) flagsLocked(p *BackgroundProcess, now time.Time) (flags []string, idle time.Duration) {
	if p.finished {
		return nil, 0
	}
	h, l := &p.health, m.limits
	if !h.busySince.IsZero() && now.Sub(h.busySince) >= l.BusyFor {
		flags = append(flags, api.ProcessFlagCPU)
	}
	if h.measured && (h.memory >= l.MemoryBytes || growing(h, now, l.GrowthWindow)) {
		flags = append(flags, api.ProcessFlagMemory)
	}
	if a, ok := m.sessions[p.Session]; ok && p.Session != "" {
		if a.turns == 0 {
			idle = now.Sub(a.last)
		}
		if a.gone || (a.turns == 0 && idle >= l.LeftAfter) {
			flags = append(flags, api.ProcessFlagLeft)
		}
	}
	return flags, idle
}

// growing: over growthFloor and up by half on a sample a whole window old.
func growing(h *procHealth, now time.Time, window time.Duration) bool {
	if len(h.memories) < 2 || h.memory < growthFloor {
		return false
	}
	base := h.memories[0]
	return !base.at.After(now.Add(-window)) && h.memory*2 >= base.bytes*3
}

// pruneSessionsLocked forgets the sessions with no process and no turn
// running; m.mu held.
func (m *ProcessManager) pruneSessionsLocked() {
	used := map[string]bool{}
	for _, p := range m.procs {
		used[p.Session] = true
	}
	for id, a := range m.sessions {
		if !used[id] && a.turns == 0 {
			delete(m.sessions, id)
		}
	}
}
