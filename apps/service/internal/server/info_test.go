package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"connectrpc.com/connect"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// The service says which version it is and what it runs from, so clients
// can tell a stale one (DSK-51a).
func TestGetServiceInfo(t *testing.T) {
	for _, tc := range []struct {
		opts []Option
		want string
	}{
		{nil, "dev"},
		{[]Option{WithVersion("1.4.0")}, "1.4.0"},
	} {
		s := New(nil, tc.opts...)
		srv := httptest.NewServer(s.Handler())
		c := pb.NewWorkspaceServiceClient(http.DefaultClient, srv.URL)
		res, err := c.GetServiceInfo(context.Background(), connect.NewRequest(&pb.GetServiceInfoRequest{}))
		srv.Close()
		s.Close()
		if err != nil {
			t.Fatal(err)
		}
		exe, _ := os.Executable()
		if res.Msg.Version != tc.want || res.Msg.Executable != exe || time.Since(res.Msg.Started.AsTime()) > time.Minute {
			t.Errorf("info = %v, want version %q, executable %q", res.Msg, tc.want, exe)
		}
	}
}
