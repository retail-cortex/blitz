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
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCPUTime(t *testing.T) {
	tests := map[string]struct {
		in   string
		want time.Duration
		bad  bool
	}{
		"macOS seconds":      {in: "0:01.50", want: 1500 * time.Millisecond},
		"macOS long minutes": {in: "309:36.35", want: 309*time.Minute + 36350*time.Millisecond},
		"Linux hours":        {in: "01:02:03", want: time.Hour + 2*time.Minute + 3*time.Second},
		"Linux days":         {in: "2-01:00:00", want: 49 * time.Hour},
		"no colon":           {in: "12", bad: true},
		"not a number":       {in: "a:01", bad: true},
		"bad seconds":        {in: "1:xx", bad: true},
		"bad days":           {in: "x-01:00:00", bad: true},
		"too many parts":     {in: "1:2:3:4", bad: true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := parseCPUTime(tt.in)
			if tt.bad {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.InDelta(t, tt.want.Seconds(), got.Seconds(), 0.001)
		})
	}
}

// A group's processes add up; lines that aren't "pgid time rss" are skipped.
func TestParseGroups(t *testing.T) {
	out := []byte("  10 0:01.00 1024\n  10 0:02.00 2048\n  11 01:00:00 4\nheader line\n x 0:01.00 1\n 12 bad 1\n 13 0:01.00 x\n")
	groups := parseGroups(out)
	assert.Equal(t, map[int]groupUsage{
		10: {CPU: 3 * time.Second, Memory: 3 << 20},
		11: {CPU: time.Hour, Memory: 4 << 10},
	}, groups)
}

// procClock is a settable time for the manager.
type procClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *procClock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }

func (c *procClock) add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

// healthRig is a manager with a fake clock and sampler, and one sleeping
// process started in session "s".
type healthRig struct {
	pm    *ProcessManager
	clock *procClock
	bp    *BackgroundProcess
	mu    sync.Mutex
	usage groupUsage
	err   error
}

func newHealthRig(t *testing.T) *healthRig {
	t.Helper()
	r := &healthRig{pm: NewProcessManager(0, 0), clock: &procClock{t: time.Unix(1_000_000, 0)}}
	r.pm.now = r.clock.now
	r.pm.sample = func() (map[int]groupUsage, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.err != nil {
			return nil, r.err
		}
		return map[int]groupUsage{r.bp.pid: r.usage}, nil
	}
	// The monitor's own ticks wait an hour: the test samples.
	r.pm.SetLimits(ProcessLimits{SampleEvery: time.Hour, LeftAfter: time.Hour})
	t.Cleanup(r.pm.Shutdown)
	var err error
	r.bp, err = r.pm.Start("s", "sleep 30", t.TempDir())
	require.NoError(t, err)
	return r
}

// step moves the clock on by d, with the group having used cpu more and
// now holding memory, and samples.
func (r *healthRig) step(d, cpu time.Duration, memory int64) {
	r.clock.add(d)
	r.mu.Lock()
	r.usage.CPU += cpu
	r.usage.Memory = memory
	r.mu.Unlock()
	r.pm.sampleOnce()
}

func (r *healthRig) info(t *testing.T) api.ProcessInfo {
	t.Helper()
	for _, p := range r.pm.Running() {
		if p.ID == r.bp.ID {
			return p
		}
	}
	t.Fatalf("process %d isn't running", r.bp.ID)
	return api.ProcessInfo{}
}

func TestProcessHealth(t *testing.T) {
	const mb = 1 << 20
	tests := map[string]struct {
		run       func(r *healthRig)
		flags     []string
		measured  bool
		cpu       float64
		memory    int64
		idleAbove time.Duration
	}{
		"fine": {
			run:      func(r *healthRig) { r.step(0, 0, 100*mb); r.step(5*time.Second, time.Second, 100*mb) },
			measured: true, cpu: 20, memory: 100 * mb,
		},
		"a core kept busy": {
			run: func(r *healthRig) {
				r.step(0, 0, mb)
				for range 25 { // 2 minutes and more at 100%
					r.step(5*time.Second, 5*time.Second, mb)
				}
			},
			flags: []string{api.ProcessFlagCPU}, measured: true, cpu: 100, memory: mb,
		},
		"busy, then quiet": {
			run: func(r *healthRig) {
				r.step(0, 0, mb)
				for range 25 {
					r.step(5*time.Second, 5*time.Second, mb)
				}
				r.step(5*time.Second, 0, mb)
			},
			measured: true, memory: mb,
		},
		"busy, not yet for long": {
			run: func(r *healthRig) {
				r.step(0, 0, mb)
				r.step(5*time.Second, 5*time.Second, mb)
				r.step(5*time.Second, 5*time.Second, mb)
			},
			measured: true, cpu: 100, memory: mb,
		},
		"over the memory limit": {
			run:   func(r *healthRig) { r.step(0, 0, 3<<30) },
			flags: []string{api.ProcessFlagMemory}, measured: true, memory: 3 << 30,
		},
		"memory growing over a window": {
			run: func(r *healthRig) {
				r.step(0, 0, 300*mb)
				for i := range 11 {
					r.step(time.Minute, 0, int64(300+20*(i+1))*mb)
				}
			},
			flags: []string{api.ProcessFlagMemory}, measured: true, memory: 520 * mb,
		},
		"memory growing, small": {
			run: func(r *healthRig) {
				r.step(0, 0, 10*mb)
				r.step(11*time.Minute, 0, 100*mb)
			},
			measured: true, memory: 100 * mb,
		},
		"a child exited: no negative cpu": {
			run: func(r *healthRig) {
				r.step(0, 10*time.Second, mb)
				r.step(5*time.Second, -8*time.Second, mb)
			},
			measured: true, memory: mb,
		},
		"not sampled here": {
			run: func(r *healthRig) {
				r.mu.Lock()
				r.err = errors.New("no ps")
				r.mu.Unlock()
				r.step(time.Second, 0, mb)
			},
		},
		"left running: no turn for long": {
			run:       func(r *healthRig) { r.clock.add(2 * time.Hour) },
			flags:     []string{api.ProcessFlagLeft},
			idleAbove: time.Hour,
		},
		"left running: its session deleted": {
			run:   func(r *healthRig) { r.pm.SessionGone("s") },
			flags: []string{api.ProcessFlagLeft},
		},
		"a turn running in its session": {
			run: func(r *healthRig) {
				r.pm.TurnStarted("s")
				r.clock.add(3 * time.Hour)
			},
		},
		"a turn ended lately": {
			run: func(r *healthRig) {
				r.clock.add(3 * time.Hour)
				done := r.pm.TurnStarted("s")
				r.clock.add(time.Minute)
				done()
				r.clock.add(time.Minute)
			},
			idleAbove: time.Minute - 1,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			r := newHealthRig(t)
			tt.run(r)
			p := r.info(t)
			assert.Equal(t, tt.flags, p.Flags)
			assert.Equal(t, tt.measured, p.Measured)
			assert.InDelta(t, tt.cpu, p.CPUPercent, 0.01)
			assert.Equal(t, tt.memory, p.MemoryBytes)
			assert.Equal(t, "s", p.Session)
			assert.Greater(t, p.IdleMs, tt.idleAbove.Milliseconds()-1)
		})
	}
}

// A process that ended has no flags or usage; with none running, the
// monitor stops, and the idle sessions are forgotten as finished
// processes are pruned.
func TestProcessHealthEnds(t *testing.T) {
	r := newHealthRig(t)
	r.step(0, 0, 3<<30)
	r.pm.SessionGone("s")
	_, err := r.pm.Kill(r.bp.ID)
	require.NoError(t, err)
	_, info, err := r.pm.Output(r.bp.ID)
	require.NoError(t, err)
	assert.Empty(t, info.Flags)
	assert.False(t, info.Measured)
	assert.Zero(t, info.MemoryBytes)

	assert.False(t, r.pm.sampleOnce(), "nothing runs: the monitor stops")
	r.pm.mu.Lock()
	defer r.pm.mu.Unlock()
	assert.False(t, r.pm.monitoring)
	r.pm.pruneSessionsLocked()
	assert.Contains(t, r.pm.sessions, "s", "kept while its process is")
	delete(r.pm.procs, r.bp.ID) // pruned
	r.pm.pruneSessionsLocked()
	assert.NotContains(t, r.pm.sessions, "s", "a session with no process is forgotten")
}

func TestTurnStartedOutsideASession(t *testing.T) {
	pm := NewProcessManager(0, 0)
	pm.TurnStarted("")()
	pm.mu.Lock()
	defer pm.mu.Unlock()
	assert.Empty(t, pm.sessions)
}

func TestProcessLimits(t *testing.T) {
	d := DefaultProcessLimits()
	tests := map[string]struct {
		in   config.ProcessesConfig
		want ProcessLimits
	}{
		"unset keeps the defaults": {want: d},
		"set": {
			in: config.ProcessesConfig{CPUPercent: 50, BusyFor: "30s", MemoryMB: 512, GrowthWindow: "1m", LeftAfter: "15m"},
			want: ProcessLimits{
				CPUPercent: 50, BusyFor: 30 * time.Second, MemoryBytes: 512 << 20, GrowthWindow: time.Minute,
				LeftAfter: 15 * time.Minute, SampleEvery: d.SampleEvery,
			},
		},
		"not a duration keeps the default": {in: config.ProcessesConfig{BusyFor: "soon"}, want: d},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			pm := NewProcessManager(0, 0)
			pm.SetLimits(ProcessLimitsFrom(tt.in))
			pm.mu.Lock()
			defer pm.mu.Unlock()
			assert.Equal(t, tt.want, pm.limits)
		})
	}
}

// A background process's output keeps its start and its latest: what a
// long-running server says now is never lost past the limit.
func TestHeadTailBuffer(t *testing.T) {
	tests := map[string]struct {
		limit  int
		writes []string
		want   string
		cut    bool
	}{
		"under the limit": {limit: 8, writes: []string{"ab", "cd"}, want: "abcd"},
		"just full":       {limit: 8, writes: []string{"abcd", "efgh"}, want: "abcdefgh"},
		"past the limit": {
			limit: 8, writes: []string{"abcd", "efgh", "ijkl"},
			want: "abcd\n\n... [output truncated: 4 bytes in the middle were discarded] ...\n\nijkl", cut: true,
		},
		"compacted many times": {
			limit: 8, writes: []string{"abcd", "1111", "2222", "3333", "4444", "wxyz"},
			want: "abcd\n\n... [output truncated: 16 bytes in the middle were discarded] ...\n\nwxyz", cut: true,
		},
		"a rune cut at the tail's start": {
			limit: 8, writes: []string{"abcd", "xé123"},
			want: "abcd\n\n... [output truncated: 2 bytes in the middle were discarded] ...\n\n123", cut: true,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			b := newHeadTailBuffer(tt.limit)
			for _, w := range tt.writes {
				n, err := b.Write([]byte(w))
				require.NoError(t, err)
				assert.Equal(t, len(w), n)
			}
			assert.Equal(t, tt.want, b.String())
			assert.Equal(t, tt.cut, b.Truncated())
		})
	}
}
