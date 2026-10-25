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
	"bufio"
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// groupUsage is what a background process and its children use: their
// CPU time so far and their resident memory (spec_processes_052 PR-10).
type groupUsage struct {
	CPU    time.Duration
	Memory int64 // bytes
}

// parseGroups reads ps's "pgid time rss" lines (rss in KiB) and adds them
// up by process group.
func parseGroups(out []byte) map[int]groupUsage {
	groups := map[int]groupUsage{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) != 3 {
			continue
		}
		pgid, err := strconv.Atoi(f[0])
		if err != nil {
			continue
		}
		cpu, err := parseCPUTime(f[1])
		if err != nil {
			continue
		}
		rss, err := strconv.ParseInt(f[2], 10, 64)
		if err != nil {
			continue
		}
		g := groups[pgid]
		g.CPU += cpu
		g.Memory += rss * 1024
		groups[pgid] = g
	}
	return groups
}

// parseCPUTime reads ps's cumulative CPU time: macOS's "mm:ss.cc" (the
// minutes unbounded) or Linux's "[dd-]hh:mm:ss".
func parseCPUTime(s string) (time.Duration, error) {
	var days int
	if d, rest, ok := strings.Cut(s, "-"); ok {
		n, err := strconv.Atoi(d)
		if err != nil {
			return 0, fmt.Errorf("cpu time %q: %w", s, err)
		}
		days, s = n, rest
	}
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, fmt.Errorf("cpu time %q: not [h:]m:s", s)
	}
	sec, err := strconv.ParseFloat(parts[len(parts)-1], 64)
	if err != nil {
		return 0, fmt.Errorf("cpu time %q: %w", s, err)
	}
	total := sec + float64(days)*86400
	for i, unit := range []float64{60, 3600}[:len(parts)-1] {
		n, err := strconv.Atoi(parts[len(parts)-2-i])
		if err != nil {
			return 0, fmt.Errorf("cpu time %q: %w", s, err)
		}
		total += float64(n) * unit
	}
	return time.Duration(total * float64(time.Second)), nil
}
