// Package daemon runs the Blitz service: every workspace a client opens,
// served over the per-user socket, and the workers' schedules.
package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/retail-cortex/blitz/apps/service/internal/server"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/engine/workers"
	"github.com/retail-cortex/blitz/pkg/socket"
)

// grace is how long turns in progress may finish when the service stops.
const grace = 10 * time.Second

// Options are how the service runs.
type Options struct {
	// Socket is where it listens (socket.DefaultSocket when empty).
	Socket string
	// Config is the configuration file ("" for the usual search).
	Config string
	// Version is reported in the diagnostic log and telemetry.
	Version string
}

// Run serves until ctx is done, then lets turns in progress finish for a
// few seconds and removes the socket. A service already answering on the
// socket is an error wrapping socket.ErrRunning.
func Run(ctx context.Context, o Options) error {
	if o.Socket == "" {
		o.Socket = socket.DefaultSocket()
	}
	cfg, err := config.Load(o.Config)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}
	warn := func(msg string) { slog.Warn(msg) }
	defer engine.StartObservability(ctx, cfg, o.Version, warn)()

	// One record of enabled workers and their runs, shared by every
	// workspace the service opens.
	store, err := workers.OpenStore(config.ExpandHome("~/.blitz/workers.json"))
	if err != nil {
		return err
	}
	runs := workers.OpenRunLog(config.ExpandHome("~/.blitz/worker-runs"))

	// Each workspace gets its own configuration: a workspace owns and
	// changes it.
	s := server.New(func(ctx context.Context, dir string) (*engine.Workspace, error) {
		cfg, err := config.Load(o.Config)
		if err != nil {
			return nil, err
		}
		cfg.Tools.WorkspaceDir = dir
		w, err := engine.Open(ctx, cfg, engine.Options{
			Streaming: true,
			Warn:      func(msg string) { slog.Warn(msg, "workspace", dir) },
			Workers:   store,
		})
		if err != nil {
			return nil, err
		}
		if merr := w.ModelErr(); merr != nil {
			slog.Warn("model unavailable", "workspace", dir, "error", engine.ModelErrorSummary(merr, cfg))
		}
		slog.Info("workspace opened", "workspace", dir)
		return w, nil
	}, server.WithScheduler(server.SchedulerConfig{Store: store, Runs: runs, MaxConcurrent: cfg.Workers.Policy.MaxConcurrent}))
	defer s.Close()

	l, err := socket.Listen(o.Socket)
	if err != nil {
		return err
	}
	defer os.Remove(o.Socket)
	if cfg.Workers.Enabled {
		s.StartScheduler(ctx)
	}
	fmt.Fprintf(os.Stderr, "Blitz service listening on %s\n", o.Socket)
	slog.Info("serve", "socket", o.Socket)
	return server.Serve(ctx, l, s.Handler(), grace)
}
