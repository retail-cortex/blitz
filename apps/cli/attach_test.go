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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/apps/service/servicetest"
	"github.com/retail-cortex/blitz/pkg/socket"
)

// The CLI attaches to a workspace the service holds, and --local on it is
// refused.
func TestAttachToTheService(t *testing.T) {
	isolate(t)
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	dir, err := os.MkdirTemp("/tmp", "cp") // socket paths must be short
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s.sock")
	t.Setenv("BLITZ_SOCKET", sock) // where the CLI looks for the service

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- servicetest.Run(ctx, sock) }()
	deadline := time.Now().Add(10 * time.Second)
	for !socket.Running(sock) {
		if time.Now().After(deadline) {
			t.Fatal("service didn't start")
		}
		time.Sleep(20 * time.Millisecond)
	}

	ws := t.TempDir()
	// The CLI attaches to the service's workspace: here it fails on the
	// service's unconfigured model (exit 1). Opening the workspace itself
	// would have failed on the lock instead (exit 2).
	if _, err := runCLI(t, "-d", ws, "--output-format", "json", "hello"); exitCodeFor(err) != exitFailure || !strings.Contains(err.Error(), "model initialization failed") {
		t.Errorf("attached one-shot: %v", err)
	}
	// --local opens it here, which the service's lock refuses.
	if _, err := runCLI(t, "--local", "-d", ws, "hello"); exitCodeFor(err) != exitUsage || !strings.Contains(err.Error(), "open elsewhere") {
		t.Errorf("--local on a workspace the service holds: %v", err)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("service: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("service didn't stop")
	}
}
