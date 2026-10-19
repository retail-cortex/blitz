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
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/wininstall"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The setup program comes out with the zip at its end, which it finds.
func TestPack(t *testing.T) {
	dir := t.TempDir()
	exe, z, out := filepath.Join(dir, "winsetup.exe"), filepath.Join(dir, "app.zip"), filepath.Join(dir, "setup.exe")
	require.NoError(t, os.WriteFile(exe, []byte("MZ"), 0o755))
	var zb bytes.Buffer
	zb.WriteString("PK\x05\x06" + string(make([]byte, 18))) // an empty zip
	require.NoError(t, os.WriteFile(z, zb.Bytes(), 0o644))
	var stderr bytes.Buffer
	require.Equal(t, 0, cmd([]string{"--exe", exe, "--zip", z, "--out", out}, &stderr), stderr.String())
	data, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Equal(t, "MZ", string(data[:2]))
	zr, err := wininstall.Payload(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)
	assert.Empty(t, zr.File)

	assert.Equal(t, 1, cmd([]string{"--exe", exe}, &stderr))
	assert.Equal(t, 1, cmd([]string{"--exe", filepath.Join(dir, "missing"), "--zip", z, "--out", out}, &stderr))
	assert.Equal(t, 2, cmd([]string{"--bogus"}, &stderr))
}
