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

package breaker

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func TestOpensBacksOffAndRecovers(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	b := New(2)
	b.SetClock(clk.now)

	ok, _ := b.Allow()
	require.True(t, ok, "new breaker refused a call")
	_, opened, _ := b.Failure()
	require.False(t, opened, "opened after one failure")
	_, opened, cd := b.Failure()
	require.True(t, opened, "second failure: opened=%v cooldown=%v", opened, cd)
	require.Equal(t, InitialCooldown, cd, "second failure: opened=%v cooldown=%v", opened, cd)
	ok, retryIn := b.Allow()
	require.False(t, ok, "open breaker: ok=%v retryIn=%v", ok, retryIn)
	require.Equal(t, InitialCooldown, retryIn, "open breaker: ok=%v retryIn=%v", ok, retryIn)

	clk.advance(InitialCooldown)
	ok, _ = b.Allow()
	require.True(t, ok, "no trial after the cooldown")
	ok, _ = b.Allow()
	require.False(t, ok, "a second caller got through during the trial")
	_, opened, cd = b.Failure()
	require.True(t, opened, "failed trial: opened=%v cooldown=%v", opened, cd)
	require.Equal(t, 2*InitialCooldown, cd, "failed trial: opened=%v cooldown=%v", opened, cd)

	for range 10 { // back-off is capped
		clk.advance(MaxCooldown)
		b.Allow()
		b.Failure()
	}
	require.Equal(t, MaxCooldown, b.cooldown, "cooldown = %v, want cap %v", b.cooldown, MaxCooldown)

	clk.advance(MaxCooldown)
	b.Allow()
	require.True(t, b.Success(), "successful trial did not report recovery")
	ok, _ = b.Allow()
	require.True(t, ok, "breaker not closed after recovery")
	require.False(t, b.Success(), "breaker not closed after recovery")
}

func TestThresholdOfOneOpensOnFirstFailure(t *testing.T) {
	b := New(0) // clamped to 1
	_, opened, _ := b.Failure()
	require.True(t, opened, "threshold 1 did not open on the first failure")
}
