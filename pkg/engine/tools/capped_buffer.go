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
	"fmt"
	"sync"
	"unicode/utf8"

	"github.com/retail-cortex/blitz/pkg/textutil"
)

// cappedBuffer is a concurrency-safe io.Writer that keeps at most limit bytes
// and silently discards the rest, so runaway output cannot exhaust memory.
// With a tail (newHeadTailBuffer), what comes past the limit replaces
// the oldest of the tail instead, so the latest output is kept too.
type cappedBuffer struct {
	mu       sync.Mutex
	buf      []byte
	limit    int
	dropped  int64
	tail     []byte // the latest output past buf, up to 2×tailCap (newHeadTailBuffer)
	tailCap  int
	tailSeen int64 // bytes written past buf
}

func newCappedBuffer(limit int) *cappedBuffer {
	return &cappedBuffer{limit: limit, buf: make([]byte, 0, min(limit, 4096))}
}

// newHeadTailBuffer keeps the first half of limit and the latest half: a
// long-running process's start and what it says now (spec_processes_052).
func newHeadTailBuffer(limit int) *cappedBuffer {
	c := newCappedBuffer(limit - limit/2)
	c.tailCap = limit / 2
	return c
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	room := c.limit - len(c.buf)
	n := 0
	if room > 0 {
		n = min(room, len(p))
		c.buf = append(c.buf, p[:n]...)
	}
	rest := p[n:]
	if c.tailCap > 0 {
		c.tailSeen += int64(len(rest))
		c.tail = append(c.tail, rest...)
		// Compacted once it doubles, so a chatty process isn't copied
		// on every write.
		if len(c.tail) > 2*c.tailCap {
			c.tail = append(c.tail[:0], c.tail[len(c.tail)-c.tailCap:]...)
		}
		return len(p), nil
	}
	c.dropped += int64(len(rest))
	return len(p), nil
}

// String returns the captured output, with a notice when bytes were dropped.
func (c *cappedBuffer) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := string(c.buf)
	if c.tailCap > 0 {
		tail := c.tail[max(0, len(c.tail)-c.tailCap):]
		dropped := c.tailSeen - int64(len(tail))
		if dropped == 0 {
			return s + string(tail)
		}
		return textutil.TrimPartialRune(s) +
			fmt.Sprintf("\n\n... [output truncated: %d bytes in the middle were discarded] ...\n\n", dropped) +
			trimLeadingPartialRune(string(tail))
	}
	if c.dropped == 0 {
		return s
	}
	return textutil.TrimPartialRune(s) +
		fmt.Sprintf("\n\n... [output truncated: %d bytes over the %d byte limit were discarded]", c.dropped, c.limit)
}

// trimLeadingPartialRune drops the bytes of a rune cut at s's start.
func trimLeadingPartialRune(s string) string {
	for i := 0; i < len(s) && i < utf8.UTFMax; i++ {
		if utf8.RuneStart(s[i]) {
			return s[i:]
		}
	}
	return s
}

// Truncated reports whether any output was discarded.
func (c *cappedBuffer) Truncated() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dropped > 0 || c.tailSeen > int64(c.tailCap)
}
