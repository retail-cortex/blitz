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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPermissionsCommand(t *testing.T) {
	app, _ := newCommandApp(t, "/permissions\n/permissions deny Bash(git push *)\n/permissions ask\n/permissions deny what(x)\n/permissions\n/permissions remove shell(git push *)\n/permissions remove shell(nope)\n/exit\n")
	out := captureStdout(t, func() { RunREPL(context.Background(), app) })
	for _, want := range []string{
		"Permission rules", "none — add some",
		"deny shell(git push *) (this session)",
		"Usage: /permissions",
		"unknown kind",
		"Removed shell(git push *)",
		"No rule shell(nope)",
	} {
		t.Run(want, func(t *testing.T) {
			assert.Contains(t, out, want, "missing %q in:\n%s", want, out)
		})
	}
}
