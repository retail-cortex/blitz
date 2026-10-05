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
	"testing"

	"github.com/stretchr/testify/assert"
)

// blitz doctor runs blitzd doctor with the run's settings, prints what it
// prints, and exits as it does; without blitzd it's a usage error.
func TestDoctorCommand(t *testing.T) {
	isolate(t)
	blitzdOnPath(t, `echo "checks: $*"; [ "$2" = "--online" ] && exit 1; exit 0`)
	out, err := runCLI(t, "doctor")
	assert.NoError(t, err)
	assert.Contains(t, out, "checks: doctor")

	out, err = runCLI(t, "-c", "/cfg", "-d", ".", "doctor")
	assert.NoError(t, err)
	assert.Contains(t, out, "checks: doctor --config /cfg --dir .")

	_, err = runCLI(t, "doctor", "--online")
	assert.Equal(t, exitFailure, exitCodeFor(err), "a check failed: %v", err)

	t.Setenv("PATH", t.TempDir())
	_, err = runCLI(t, "doctor")
	assert.Equal(t, exitUsage, exitCodeFor(err), "no blitzd: %v", err)
}

// Every setting of the run reaches blitzd doctor.
func TestDoctorArgs(t *testing.T) {
	g := &globalFlags{config: "/c", dir: "/w", model: "m", agent: "a", agency: "high", pluginDirs: []string{"/p"}, addDirs: []string{"/x"}}
	assert.Equal(t, []string{"doctor", "--config", "/c", "--dir", "/w", "--model", "m", "--agent", "a", "--agency", "high", "--plugin-dir", "/p", "--add-dir", "/x", "--online"}, doctorArgs(g, true))
	assert.Equal(t, []string{"doctor"}, doctorArgs(&globalFlags{}, false))
}
