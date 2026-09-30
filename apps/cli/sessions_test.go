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
	"os"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionsExport(t *testing.T) {
	isolate(t)
	store, err := session.NewStorage(config.DefaultConfig().Session.StorageDir)
	require.NoError(t, err)
	rec, err := store.CreateSession("", "Fix the build", "blitz")
	require.NoError(t, err)
	require.NoError(t, store.AddMessage("user", "why does it fail?"))
	require.NoError(t, store.AddMessage("model", "A missing import."))

	out, err := runCLI(t, "sessions", "export", rec.ID)
	require.NoError(t, err)
	assert.Contains(t, out, "# Fix the build")
	assert.Contains(t, out, "why does it fail?")
	assert.Contains(t, out, "A missing import.")

	file := filepath.Join(t.TempDir(), "s.md")
	_, err = runCLI(t, "sessions", "export", rec.ID, "-o", file)
	require.NoError(t, err)
	data, _ := os.ReadFile(file)
	assert.Contains(t, string(data), "A missing import.")

	_, err = runCLI(t, "sessions", "export", "nope")
	assert.Equal(t, exitUsage, exitCodeFor(err))
}

func TestForkAndNameFlags(t *testing.T) {
	isolate(t)
	for name, args := range map[string][]string{
		"fork without resume": {"--fork", "hi"},
		"name with continue":  {"--name", "x", "--continue", "hi"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := runCLI(t, args...)
			assert.Equal(t, exitUsage, exitCodeFor(err), "%v", err)
		})
	}
}
