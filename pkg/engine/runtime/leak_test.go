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

package runtime

import (
	"context"
	"fmt"
	"os"
	"testing"

	"go.uber.org/goleak"
)

// No test may leave a goroutine running. The shared test telemetry lives
// for the whole process, so it is shut down before the check. Tests get a
// home directory of their own: what Blitz keeps under ~/.blitz
// (checkpoints, approvals) must not reach the real one.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "blitz-runtime-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Setenv("HOME", home)
	code := m.Run()
	os.RemoveAll(home)
	tel, _ := testTelemetry()
	_ = tel.Shutdown(context.Background())
	if code == 0 {
		if err := goleak.Find(); err != nil {
			fmt.Fprintf(os.Stderr, "goleak: %v\n", err)
			code = 1
		}
	}
	os.Exit(code)
}
