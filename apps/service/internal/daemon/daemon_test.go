package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/socket"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// The service answers over its socket, opens workspaces on demand,
// refuses a second service on the socket, and removes the socket when
// stopped.
func TestRun(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
	for _, k := range []string{"GEMINI_API_KEY", "GOOGLE_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "LLM_PROVIDER"} {
		t.Setenv(k, "") // no real model
	}
	t.Chdir(t.TempDir())
	dir, err := os.MkdirTemp("/tmp", "bd") // socket paths must be short
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s.sock")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, Options{Socket: sock}) }()
	for deadline := time.Now().Add(10 * time.Second); !socket.Running(sock); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("service didn't start")
		}
	}

	c := pb.NewWorkspaceServiceClient(socket.Client(sock), socket.BaseURL)
	agents, err := c.ListAgents(context.Background(), connect.NewRequest(&pb.ListAgentsRequest{Workspace: t.TempDir()}))
	if err != nil || len(agents.Msg.Agents) == 0 {
		t.Fatalf("list agents: %v %v", agents, err)
	}
	if err := Run(context.Background(), Options{Socket: sock}); !errors.Is(err, socket.ErrRunning) {
		t.Errorf("second service: %v", err)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("serve: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("service didn't stop")
	}
	if _, err := os.Stat(sock); !os.IsNotExist(err) {
		t.Error("socket left behind")
	}
}
