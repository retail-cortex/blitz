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
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"extra"}, {"--nope"}} {
		code := run(context.Background(), args)
		assert.Equal(t, exitUsage, code, "blitzd %v: exit %d, want %d", args, code, exitUsage)
	}
	code := run(context.Background(), []string{"--version"})
	assert.Equal(t, 0, code, "blitzd --version: exit %d", code)
}

// --license shows the NOTICE, the Apache License or the third-party
// notices, and exits without starting the service.
func TestLicense(t *testing.T) {
	for arg, want := range map[string]string{
		"--license":             "Third-party notices: blitzd --license=third-party",
		"--license=full":        "Apache License",
		"--license=third-party": "THIRD-PARTY NOTICES",
	} {
		t.Run(arg, func(t *testing.T) {
			r, w, _ := os.Pipe()
			old := os.Stdout
			os.Stdout = w
			// Read while blitzd writes: the notices outgrow a pipe's buffer.
			read := make(chan []byte)
			go func() { b, _ := io.ReadAll(r); read <- b }()
			code := run(context.Background(), []string{arg})
			w.Close()
			os.Stdout = old
			out := <-read
			assert.Equal(t, 0, code, "blitzd %s: exit %d, no %q in %.200s", arg, code, want, out)
			assert.Contains(t, string(out), want, "blitzd %s: exit %d, no %q in %.200s", arg, code, want, out)
		})
	}
	code := run(context.Background(), []string{"--license=bogus"})
	assert.Equal(t, exitUsage, code, "--license=bogus: exit %d", code)
}

// A service that can't start exits with 1.
func TestFailureExitCode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
	conf := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(conf, ".env.toml"), []byte("[blitz\n"), 0o600))
	code := run(context.Background(), []string{"--config", conf, "--socket", filepath.Join(t.TempDir(), "s.sock")})
	assert.Equal(t, exitFailure, code, "blitzd with bad settings: exit %d", code)
}
