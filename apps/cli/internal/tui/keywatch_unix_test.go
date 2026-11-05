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

//go:build darwin || linux

package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// A read a signal interrupted is tried again, not taken as the end of input.
func TestTTYKeysReadRetriesEINTR(t *testing.T) {
	saved := sysRead
	t.Cleanup(func() { sysRead = saved })
	calls := 0
	sysRead = func(_ int, p []byte) (int, error) {
		calls++
		if calls == 1 {
			return 0, unix.EINTR
		}
		return copy(p, "x"), nil
	}
	buf := make([]byte, 4)
	n, err := (&ttyKeys{}).read(buf)
	require.NoError(t, err)
	assert.Equal(t, "x", string(buf[:n]))
	assert.Equal(t, 2, calls)
}
