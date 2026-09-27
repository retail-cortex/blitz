// blitzd is the per-user Blitz service: it holds every workspace a
// client opens (the desktop app, and the CLI when it attaches) and runs
// the workers' schedules. `blitz service install` starts it at login.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/retail-cortex/blitz/apps/service/internal/daemon"
	"github.com/retail-cortex/blitz/pkg/socket"
	"github.com/spf13/cobra"
)

// version is set at build time.
var version = "2.0.0-go"

// Exit codes, as the CLI's: 1 for a failure, 2 for a usage error (such as
// a service already running on the socket).
const (
	exitFailure = 1
	exitUsage   = 2
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:]))
}

func run(ctx context.Context, args []string) int {
	var o daemon.Options
	cmd := &cobra.Command{
		Use:   "blitzd",
		Short: "The Blitz service: every workspace, over a Unix socket",
		Long: `Runs the per-user Blitz service. Clients (the desktop app, and the CLI
when it attaches) reach it over a Unix socket only this user can open. It
opens a workspace when a client first names it; a workspace open here can't
also be opened by a separate CLI. "blitz service install" starts it at
every login.`,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("%w: blitzd takes no arguments", errUsage)
			}
			return nil
		},
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			o.Version = version
			return daemon.Run(ctx, o)
		},
	}
	cmd.Flags().StringVar(&o.Socket, "socket", "", "Unix socket to listen on (default ~/.blitz/run/blitz.sock, or $BLITZ_SOCKET)")
	cmd.Flags().StringVar(&o.Config, "config", "", "configuration file (default: the usual search, as the CLI's)")
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return fmt.Errorf("%w: %v", errUsage, err) })
	cmd.SetArgs(args)
	if err := cmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "blitzd:", err)
		if errors.Is(err, socket.ErrRunning) || errors.Is(err, errUsage) {
			return exitUsage
		}
		return exitFailure
	}
	return 0
}

// errUsage marks a bad flag or argument.
var errUsage = errors.New("usage")
