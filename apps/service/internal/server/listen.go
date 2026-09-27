package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"
)

// Serve serves h on l (HTTP/1.1, and HTTP/2 without TLS for gRPC clients)
// until ctx is done, then stops accepting and waits up to grace for calls
// in progress.
func Serve(ctx context.Context, l net.Listener, h http.Handler, grace time.Duration) error {
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
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
