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

//go:build unix

package tools

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The real sampler finds a background process's group, and the monitor
// measures it.
func TestSampleGroups(t *testing.T) {
	pm := NewProcessManager(0, 0)
	t.Cleanup(pm.Shutdown)
	pm.SetLimits(ProcessLimits{SampleEvery: 20 * time.Millisecond})
	bp, err := pm.Start("s", "sleep 30", t.TempDir())
	require.NoError(t, err)
	groups, err := sampleGroups()
	require.NoError(t, err)
	assert.Contains(t, groups, bp.pid, "its group is sampled")
	assert.Eventually(t, func() bool {
		_, info, err := pm.Output(bp.ID)
		return err == nil && info.Measured && info.MemoryBytes > 0
	}, 5*time.Second, 20*time.Millisecond)
}
