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

package docx

import (
	"strings"
	"testing"
	"time"
)

// Math that's malformed or odd ends, whatever it is: a stray &, \\,
// \right, \end, an unclosed group or environment, nothing after ^.
func TestMathAlwaysEnds(t *testing.T) {
	for _, m := range []string{
		`$a & b$`, `$\\$`, `$a \\ b$`, `$\right)$`, `$\end{x}$`, `$\end$`, `$}$`, `$}}}$`,
		`$\left($`, `$\left(\right$`, `$x^$`, `$x_$`, `$x^{$`, `$\frac$`, `$\frac{a}$`, `$\sqrt[$`,
		`$\begin{array}$`, `$\begin{matrix} a & b \\ c$`, `$\begin{aligned}$`, `$\sum_$`, `$\lim$`,
		`$$\begin{cases} a & \end{cases}$$`, `$''''$`, `$\limits$`, `$` + strings.Repeat("{", 500) + `$`,
		`$\operatorname$`, `$\text{$`, `$\mathbb$`, `$\overset$`, `$\binom{a}$`, `$\hat$`, `$\\\\&&&$`,
	} {
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, _ = Render([]byte(m), Options{})
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("Render didn't end for %q", m)
		}
	}
}
