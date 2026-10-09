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

// Package daemon runs the Blitz service: every workspace a client opens,
// served over the per-user socket, and the workers' schedules.
package daemon

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/apps/service/internal/server"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/engine/workers"
	"github.com/retail-cortex/blitz/pkg/observability"
	"github.com/retail-cortex/blitz/pkg/shellpath"
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
	// IdleExit stops the service after this long with nothing to do (no
	// request, turn or background run); 0 never. For a service a client
	// started on demand, not the login item's.
	IdleExit time.Duration
	// ExitWith stops the service when it reads to its end: the stdin of
	// a private service, a pipe from the client that started it, which
	// closes when that client exits, however it exits. Nil: never.
	ExitWith io.Reader
	// Run are one client's settings for every workspace a private service
	// opens (the CLI's flags for that run).
	Run RunOverrides
	// Private marks a service a client started for a run of its own: it
	// serves that client only, and never runs the workers' schedules (the
	// shared service does; a private one would run them twice, and open
	// every workspace with an enabled worker, keeping it from the others).
	Private bool
	// AdoptPath gives the service the user's login shell's PATH at start:
	// blitzd's, started by the tray, launchd or systemd with the system's
	// bare one (spec_service_021 SVC-57). A private service has its
	// terminal's already, and tests' services keep theirs.
	AdoptPath bool
}

// schedules reports whether the service runs the workers' schedules.
func (o Options) schedules(cfg *config.Config) bool { return cfg.Workers.Enabled && !o.Private }

// RunOverrides are a run's settings over the configuration: what the
// CLI's flags set, for the private service it starts for that run.
type RunOverrides struct {
	Model, Agent, Agency string
	// PluginDirs load more plugins; AddDirs are more read-write roots
	// (as sandbox.allowed_paths).
	PluginDirs, AddDirs []string
	// SessionDir keeps sessions elsewhere (--no-session-persistence: a
	// folder the CLI removes).
	SessionDir string
	// TrustProject trusts the project's settings for this run.
	TrustProject bool
	// AppendSystemPrompt is added to the agent's instructions.
	AppendSystemPrompt string
}

// apply sets the overrides on a workspace's configuration.
func (r RunOverrides) apply(cfg *config.Config) {
	if r.Model != "" {
		cfg.Blitz.DefaultModel = r.Model
	}
	if r.Agent != "" {
		cfg.Blitz.DefaultAgent = r.Agent
	}
	if r.Agency != "" {
		cfg.Blitz.AgencyLevel = strings.ToLower(r.Agency)
	}
	cfg.Plugins.Dirs = append(cfg.Plugins.Dirs, r.PluginDirs...)
	cfg.Sandbox.AllowedPaths = append(cfg.Sandbox.AllowedPaths, r.AddDirs...)
	if r.SessionDir != "" {
		cfg.Session.StorageDir = r.SessionDir
	}
}

// adoptUserPath gives the service the login shell's PATH; tests replace it.
var adoptUserPath = shellpath.AdoptUserPath

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
	// Started by the tray, launchd or systemd, the service has the system's
	// bare PATH: the terminal's is the one the user's tools and language
	// servers are on.
	if o.AdoptPath {
		if n := adoptUserPath(); n > 0 {
			slog.Info("PATH from the login shell", "added", n)
		}
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

	ctx, stop := context.WithCancel(ctx)
	defer stop()

	// Each workspace gets its own configuration: a workspace owns and
	// changes it.
	s := server.New(func(ctx context.Context, dir string) (*engine.Workspace, error) {
		cfg, err := config.LoadWorkspace(o.Config, dir) // its own settings over the global ones
		if err != nil {
			return nil, err
		}
		cfg.Tools.WorkspaceDir = dir
		o.Run.apply(cfg)
		w, err := engine.Open(ctx, cfg, engine.Options{
			Streaming:          true,
			Warn:               func(msg string) { slog.Warn(msg, "workspace", dir) },
			Workers:            store,
			TrustProject:       o.Run.TrustProject,
			AppendSystemPrompt: o.Run.AppendSystemPrompt,
		})
		if err != nil {
			return nil, err
		}
		if merr := w.ModelErr(); merr != nil {
			slog.Warn("model unavailable", "workspace", dir, "error", engine.ModelErrorSummary(merr, cfg))
		}
		slog.Info("workspace opened", "workspace", dir)
		return w, nil
	}, server.WithScheduler(server.SchedulerConfig{Store: store, Runs: runs, MaxConcurrent: cfg.Workers.Policy.MaxConcurrent}), server.WithVersion(o.Version), server.WithConfigDir(o.Config), server.WithLogDir(logDir(cfg)), server.WithShutdown(stop))
	defer s.Close()

	l, err := socket.Listen(o.Socket)
	if err != nil {
		return err
	}
	defer os.Remove(o.Socket)
	go watch(ctx, stop, o, s.IdleFor)
	if o.schedules(cfg) {
		s.StartScheduler(ctx)
	}
	fmt.Fprintf(os.Stderr, "Blitz service listening on %s\n", o.Socket)
	slog.Info("serve", "socket", o.Socket)
	return server.Serve(ctx, l, s.Handler(), grace)
}

// watchEvery is how often watch looks at the service's idle time.
var watchEvery = time.Second

// watch calls stop when the service has been idle for o.IdleExit, or when
// o.ExitWith reaches its end (the client that started it has gone).
func watch(ctx context.Context, stop context.CancelFunc, o Options, idleFor func() time.Duration) {
	if o.ExitWith != nil {
		go func() {
			_, _ = io.Copy(io.Discard, o.ExitWith)
			slog.Info("exit: the client that started the service has gone")
			stop()
		}()
	}
	if o.IdleExit <= 0 {
		return
	}
	t := time.NewTicker(watchEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if idleFor() >= o.IdleExit {
				slog.Info("exit: idle", "after", o.IdleExit)
				stop()
				return
			}
		}
	}
}

// logDir is where the service's diagnostic log is, "" when it's off.
func logDir(cfg *config.Config) string {
	if _, on, err := observability.ParseLevel(cfg.Log.Level); err != nil || !on {
		return ""
	}
	return config.ExpandHome(cfg.Log.Dir)
}
