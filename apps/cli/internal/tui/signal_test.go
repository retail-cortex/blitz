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
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A stopped watcher never takes the next signal: the prompt's watcher is
// stopped as a turn starts, and the Ctrl+C that follows is the turn's.
func TestCancelOnSignalStoppedWatcherLetsGo(t *testing.T) {
	sigs := make(chan os.Signal, 1)
	for i := range 1000 {
		_, stopIdle := cancelOnSignal(context.Background(), sigs)
		stopIdle()
		turnCtx, stopTurn := cancelOnSignal(context.Background(), sigs)
		sigs <- os.Interrupt
		select {
		case <-turnCtx.Done():
		case <-time.After(5 * time.Second):
			t.Fatalf("round %d: the turn never saw the interrupt: a stopped watcher took it", i)
		}
		stopTurn()
		require.Empty(t, sigs, "round %d: the signal is still queued", i)
	}
}
