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

package tray

import (
	"context"
	"net/http"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/socket"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Asking the service how it is, as the tray does every 2 s, leaves nothing
// behind: no connection, no goroutine, in the tray or the service.
func TestProbeLeavesNothingBehind(t *testing.T) {
	sock := filepath.Join(shortTemp(t), "blitz.sock")
	ln, err := socket.Listen(sock)
	require.NoError(t, err)
	mux := http.NewServeMux()
	mux.Handle(pb.NewWorkspaceServiceHandler(pb.UnimplementedWorkspaceServiceHandler{}))
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { srv.Close() })

	probe := func() {
		Probe(context.Background(), sock, func() bool { return false })
		ServiceLogDir(context.Background(), sock)
	}
	probe() // the first opens the connection the rest reuse
	time.Sleep(50 * time.Millisecond)
	before := runtime.NumGoroutine()
	for range 100 {
		probe()
	}
	time.Sleep(50 * time.Millisecond)
	assert.LessOrEqual(t, runtime.NumGoroutine(), before+5, "goroutines grew with each probe")
}
