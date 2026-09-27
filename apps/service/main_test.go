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
	"strings"
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

// --license shows the NOTICE, the Apache License or the third-party
// notices, and exits without starting the service.
func TestLicense(t *testing.T) {
	for arg, want := range map[string]string{
		"--license":             "Third-party notices: blitzd --license=third-party",
		"--license=full":        "Apache License",
		"--license=third-party": "THIRD-PARTY NOTICES",
	} {
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
		if code != 0 || !strings.Contains(string(out), want) {
			t.Errorf("blitzd %s: exit %d, no %q in %.200s", arg, code, want, out)
		}
	}
	if code := run(context.Background(), []string{"--license=bogus"}); code != exitUsage {
		t.Errorf("--license=bogus: exit %d", code)
	}
}
