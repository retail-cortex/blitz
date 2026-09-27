package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/retail-cortex/blitz/pkg/socket"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/retail-cortex/blitz/pkg/client"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/i18n"
)

// globalFlags are shared by the root command and subcommands.
type globalFlags struct {
	config, dir, model, agent, agency string
	trustWorkspace                    bool
}

// loadConfig loads trusted configuration and applies flag overrides,
// including --dir as the workspace. The process's working directory is
// left alone: the workspace is always named explicitly.
func loadConfig(f *globalFlags) (*config.Config, error) {
	var dir string
	if f.dir != "" {
		abs, err := filepath.Abs(config.ExpandHome(f.dir))
		if err == nil {
			var info os.FileInfo
			if info, err = os.Stat(abs); err == nil && !info.IsDir() {
				err = fmt.Errorf("%s is not a directory", abs)
			}
		}
		if err != nil {
			return nil, withCode(exitUsage, fmt.Errorf("cannot use --dir: %w", err))
		}
		dir = abs
	}
	cfg, err := config.Load(f.config)
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}
	if f.model != "" {
		cfg.Blitz.DefaultModel = f.model
	}
	if f.agent != "" {
		cfg.Blitz.DefaultAgent = f.agent
	}
	if f.agency != "" {
		cfg.Blitz.AgencyLevel = strings.ToLower(f.agency)
	}
	if f.trustWorkspace {
		cfg.Blitz.TrustWorkspace = true
	}
	if dir != "" {
		cfg.Tools.WorkspaceDir = dir
	}
	return cfg, nil
}

// openBackend attaches to the workspace in the Blitz service when one
// is running (unless local), and otherwise opens it in this process. It
// also returns the interface's translation catalogs, which are always this
// process's, and whether it attached.
func openBackend(ctx context.Context, cfg *config.Config, local, streaming bool, warn func(string)) (api.Backend, *i18n.Bundle, bool, error) {
	sock := socket.DefaultSocket()
	if !local && socket.Running(sock) {
		r, err := client.Attach(ctx, sock, cfg.Tools.WorkspaceDir, warn)
		if err != nil {
			return nil, nil, false, fmt.Errorf("attaching to the Blitz service at %s: %w (--local runs without it)", sock, err)
		}
		return r, engine.SetupLocale(cfg, warn), true, nil
	}
	w, err := engine.Open(ctx, cfg, engine.Options{Streaming: streaming, Warn: warn})
	if errors.Is(err, api.ErrWorkspaceBusy) {
		return nil, nil, false, withCode(exitUsage, fmt.Errorf("%w (another blitz has it open)", err))
	}
	if err != nil {
		return nil, nil, false, err
	}
	return w, w.Locales(), false, nil
}
