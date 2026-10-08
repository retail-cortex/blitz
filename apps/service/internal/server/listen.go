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

package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"
)

// idleTimeout closes a client's connection after this long with no
// request: a client that makes one for each call and drops it (an old
// tray did, every 2 s) would otherwise hold one open, and its goroutine,
// for as long as the service runs. Streams in progress aren't idle.
var idleTimeout = 2 * time.Minute

// Serve serves h on l (HTTP/1.1, and HTTP/2 without TLS for gRPC clients)
// until ctx is done, then stops accepting and waits up to grace for calls
// in progress.
func Serve(ctx context.Context, l net.Listener, h http.Handler, grace time.Duration) error {
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: idleTimeout}
	srv.Protocols = new(http.Protocols)
	srv.Protocols.SetHTTP1(true)
	srv.Protocols.SetUnencryptedHTTP2(true)
	done := make(chan error, 1)
	go func() { done <- srv.Serve(l) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	err := srv.Shutdown(sctx)
	if errors.Is(err, context.DeadlineExceeded) {
		err = srv.Close() // turns still running are cut off
	}
	<-done
	return err
}
