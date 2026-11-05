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

package main

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Asked to stop (SIGTERM: a cloud runner's stop, a service manager's), the
// service and a job's run get a cancelled context rather than being
// killed, so they can save what they did.
func TestUntilStopped(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		t.Run(sig.String(), func(t *testing.T) {
			ctx, stop := untilStopped(context.Background())
			defer stop()
			require.NoError(t, syscall.Kill(os.Getpid(), sig))
			select {
			case <-ctx.Done():
			case <-time.After(2 * time.Second):
				t.Fatal("not cancelled")
			}
		})
	}
}
