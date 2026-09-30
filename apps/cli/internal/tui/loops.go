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

package tui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/pkg/i18n"
)

// Loops (spec_parity_027 PAR-SES-31): /loop <interval> <prompt> sends a
// prompt again every interval while the REPL stays open. A loop that comes
// due while you're at the prompt runs at once (an unfinished line is
// dropped); during a turn it waits for the turn to end. Loops end with the
// REPL; durable schedules are workers.

// minLoop is the shortest interval.
const minLoop = time.Minute

// errLoopDue interrupts the prompt when a loop comes due.
var errLoopDue = errors.New("a loop is due")

type sessionLoop struct {
	n      int
	every  time.Duration
	prompt string
	next   time.Time
}

// loops are the REPL's loops.
type loops struct {
	mu   sync.Mutex
	list []*sessionLoop
	seq  int
	now  func() time.Time // replaced in tests
}

func (l *loops) clock() time.Time {
	if l.now != nil {
		return l.now()
	}
	return time.Now()
}

func (l *loops) add(every time.Duration, prompt string) *sessionLoop {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq++
	s := &sessionLoop{n: l.seq, every: every, prompt: prompt, next: l.clock().Add(every)}
	l.list = append(l.list, s)
	return s
}

func (l *loops) stop(n int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i, s := range l.list {
		if s.n == n {
			l.list = append(l.list[:i], l.list[i+1:]...)
			return true
		}
	}
	return false
}

func (l *loops) all() []sessionLoop {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]sessionLoop, len(l.list))
	for i, s := range l.list {
		out[i] = *s
	}
	return out
}

// takeDue is a loop that's due, rescheduled from now; nil when none is.
func (l *loops) takeDue() *sessionLoop {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clock()
	for _, s := range l.list {
		if !s.next.After(now) {
			s.next = now.Add(s.every)
			c := *s
			return &c
		}
	}
	return nil
}

// waitCtx is ctx, cancelled with errLoopDue when the next loop comes due.
func (l *loops) waitCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	l.mu.Lock()
	var next time.Time
	for _, s := range l.list {
		if next.IsZero() || s.next.Before(next) {
			next = s.next
		}
	}
	l.mu.Unlock()
	cctx, cancel := context.WithCancelCause(ctx)
	if next.IsZero() {
		return cctx, func() { cancel(nil) }
	}
	t := time.AfterFunc(max(0, next.Sub(l.clock())), func() { cancel(errLoopDue) })
	return cctx, func() { t.Stop(); cancel(nil) }
}

// parseInterval reads 10m, 1h30m, or a number of minutes.
func parseInterval(s string) (time.Duration, error) {
	if n, err := strconv.Atoi(s); err == nil {
		return time.Duration(n) * time.Minute, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	if d < minLoop {
		return 0, fmt.Errorf("at least %s", minLoop)
	}
	return d, nil
}

// cmdLoop is /loop: start, list and stop loops.
func cmdLoop(args []string, app *App) {
	switch {
	case len(args) == 0:
		list := app.loops.all()
		if len(list) == 0 {
			fmt.Println(i18n.T("loop.none"))
			return
		}
		fmt.Printf("%s%s%s\n", Bold, i18n.T("loop.list"), Reset)
		for _, s := range list {
			fmt.Printf("  %s\n", i18n.T("loop.item", "n", s.n, "interval", s.every, "prompt", safe(s.prompt), "next", s.next.Format("15:04")))
		}
	case args[0] == "stop" && len(args) == 2:
		n, err := strconv.Atoi(strings.TrimPrefix(args[1], "#"))
		if err != nil || !app.loops.stop(n) {
			fmt.Println(i18n.T("loop.usage"))
			return
		}
		fmt.Printf("%s✓ %s%s\n", Green, i18n.T("loop.stopped", "n", n), Reset)
	case len(args) >= 2:
		every, err := parseInterval(args[0])
		if err != nil {
			fmt.Println(i18n.T("loop.usage"))
			return
		}
		s := app.loops.add(every, strings.Join(args[1:], " "))
		fmt.Printf("%s✓ %s%s\n", Green, i18n.T("loop.started", "n", s.n, "prompt", safe(s.prompt), "interval", every), Reset)
	default:
		fmt.Println(i18n.T("loop.usage"))
	}
}
