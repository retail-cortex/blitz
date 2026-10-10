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
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Killing a command kills what it started too: cmd starts ping, which
// holds the output pipe open, so Wait returns only once both are gone
// (VE-46). Without the job, ping would run its 30 seconds.
func TestKillTakesTheWholeJob(t *testing.T) {
	var out bytes.Buffer
	var e *ExecEnv
	cmd, err := e.command(context.Background(), []string{"cmd", "/c", "ping -n 30 127.0.0.1"})
	require.NoError(t, err)
	cmd.Stdout = &out
	require.NoError(t, cmd.Start())
	time.Sleep(500 * time.Millisecond) // ping is running
	start := time.Now()
	require.NoError(t, killProcessGroup(cmd.Cmd))
	_ = cmd.Wait()
	assert.Less(t, time.Since(start), 10*time.Second, "the child outlived the kill")
}

// Releasing a finished command closes its job, killing anything it left.
func TestReleaseClosesTheJob(t *testing.T) {
	var e *ExecEnv
	cmd, err := e.command(context.Background(), []string{"cmd", "/c", "exit 0"})
	require.NoError(t, err)
	require.NoError(t, cmd.Run())
	_, held := jobs.Load(cmd.Cmd)
	assert.False(t, held, "released with the command")
}
