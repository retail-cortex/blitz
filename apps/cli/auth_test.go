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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// blitz auth: each provider's status, a sign-in with its tool (the URL
// and device code shown), a sign-out; what can't work is a usage error.
func TestAuthCommand(t *testing.T) {
	isolate(t)
	t.Setenv("SHELL", filepath.Join(t.TempDir(), "no-shell")) // PATH only: not the developer's tools
	adc := filepath.Join(t.TempDir(), "adc.json")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", adc)
	bin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bin, "gcloud"), []byte(`#!/bin/sh
case "$*" in
  *login*) echo "Waiting for you"; echo "Opening https://accounts.google.com/auth?x=1"; echo '{"type":"authorized_user"}' > "$GOOGLE_APPLICATION_CREDENTIALS" ;;
  *revoke*) rm -f "$GOOGLE_APPLICATION_CREDENTIALS" ;;
esac
`), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(bin, "aws"), []byte("#!/bin/sh\necho \"Enter WXYZ-1234 at https://device.sso.aws/\"\nexit 1\n"), 0o755))
	t.Setenv("PATH", bin+":/usr/bin:/bin")

	out, err := runCLI(t, "auth", "status")
	require.NoError(t, err)
	assert.Contains(t, out, "google")
	assert.Contains(t, out, "not signed in (blitz auth login google)")
	assert.Contains(t, out, "az isn't installed")

	out, err = runCLI(t, "auth", "login", "google")
	require.NoError(t, err, out)
	assert.Contains(t, out, "  Waiting for you")
	assert.Contains(t, out, "If the browser didn't open: https://accounts.google.com/auth?x=1")
	assert.Contains(t, out, "✓ google: signed in")

	out, err = runCLI(t, "auth", "login", "aws", "--profile", "dev")
	assert.Equal(t, exitFailure, exitCodeFor(err), "the tool failed: %v", err)
	assert.Contains(t, out, "Code: WXYZ-1234")

	out, err = runCLI(t, "auth", "logout", "google")
	require.NoError(t, err)
	assert.Contains(t, out, "not signed in")

	for _, args := range [][]string{{"auth", "login", "openai"}, {"auth", "login", "azure"}, {"auth", "logout", "aws", "--profile", "x y"}} {
		_, err := runCLI(t, args...)
		assert.Equal(t, exitUsage, exitCodeFor(err), "%v: %v", args, err)
	}

	// A workspace that isn't there, and a service that can't start.
	for _, args := range [][]string{{"-d", "/definitely/not/here", "auth", "status"}, {"-d", "/definitely/not/here", "auth", "login", "google"}, {"-d", "/definitely/not/here", "auth", "logout", "google"}} {
		_, err := runCLI(t, args...)
		assert.Equal(t, exitUsage, exitCodeFor(err), "%v: %v", args, err)
	}
	file := filepath.Join(t.TempDir(), "a-file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	t.Setenv("BLITZ_SOCKET", filepath.Join(file, "s.sock"))
	_, err = runCLI(t, "auth", "status")
	assert.ErrorContains(t, err, "starting the Blitz service")
}
