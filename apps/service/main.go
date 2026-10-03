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
	"github.com/retail-cortex/blitz/pkg/legal"
	"github.com/retail-cortex/blitz/pkg/pdftext"
	"github.com/retail-cortex/blitz/pkg/socket"
	"github.com/spf13/cobra"
)

// version is set when a release is built (the git tag).
var version = "dev"

// Exit codes, as the CLI's: 1 for a failure, 2 for a usage error (such as
// a service already running on the socket).
const (
	exitFailure = 1
	exitUsage   = 2
)

func main() {
	// A PDF's text is read in a child: this program, run again.
	pdftext.MaybeServe()
	pdftext.UseHelper()
	os.Exit(run(context.Background(), os.Args[1:]))
}

func run(ctx context.Context, args []string) int {
	var o daemon.Options
	var license string
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
			if cmd.Flags().Changed("license") {
				return showLicense(cmd, license)
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			o.Version = version
			return daemon.Run(ctx, o)
		},
	}
	cmd.Flags().StringVar(&o.Socket, "socket", "", "Unix socket to listen on (default ~/.blitz/run/blitz.sock, or $BLITZ_SOCKET)")
	cmd.Flags().StringVar(&o.Config, "config", "", "configuration file (default: the usual search, as the CLI's)")
	cmd.Flags().StringVar(&license, "license", "", "show the license and exit: the NOTICE, or =full (the Apache License) or =third-party (the notices of the software blitzd includes)")
	cmd.Flags().Lookup("license").NoOptDefVal = "notice"
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

// showLicense prints the license text --license asks for.
func showLicense(cmd *cobra.Command, which string) error {
	var text string
	switch which {
	case "notice":
		text = legal.Summary("blitzd --license=")
	case "full":
		text = legal.License
	case "third-party":
		text = legal.ThirdParty
	default:
		return fmt.Errorf("%w: --license=%s: use full or third-party", errUsage, which)
	}
	_, err := fmt.Fprint(cmd.OutOrStdout(), text)
	return err
}

// errUsage marks a bad flag or argument.
var errUsage = errors.New("usage")
