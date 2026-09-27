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

package main

import (
	"context"
	"testing"
)

func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"extra"}, {"--nope"}} {
		if code := run(context.Background(), args); code != exitUsage {
			t.Errorf("blitzd %v: exit %d, want %d", args, code, exitUsage)
		}
	}
	if code := run(context.Background(), []string{"--version"}); code != 0 {
		t.Errorf("blitzd --version: exit %d", code)
	}
}
