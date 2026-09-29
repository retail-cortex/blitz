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
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenFolder(t *testing.T) {
	var opened []string
	orig := openCommand
	openCommand = func(dir string) *exec.Cmd {
		opened = append(opened, dir)
		return exec.Command("true")
	}
	t.Cleanup(func() { openCommand = orig })

	dir := t.TempDir()
	file := filepath.Join(dir, "run.sh")
	require.NoError(t, os.WriteFile(file, []byte("#!/bin/sh\n"), 0o755))
	a := &App{}
	for _, tc := range []struct {
		name, path string
		ok         bool
	}{
		{"a folder", dir, true},
		{"a file", file, false},
		{"nothing there", filepath.Join(dir, "none"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opened = nil
			err := a.OpenFolder(tc.path)
			if tc.ok {
				assert.NoError(t, err)
				assert.Equal(t, []string{tc.path}, opened)
			} else {
				assert.Error(t, err)
				assert.Empty(t, opened, "opened %v", opened)
			}
		})
	}
}
