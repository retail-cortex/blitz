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
	"strings"
	"syscall"

	"github.com/retail-cortex/blitz/apps/service/internal/daemon"
	"github.com/retail-cortex/blitz/apps/service/internal/doctor"
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
	var exitWithStdin bool
	var promptFile string
	cmd.Flags().DurationVar(&o.IdleExit, "idle-exit", 0, "stop after this long with nothing to do (a service a client started on demand); 0 never")
	cmd.Flags().BoolVar(&exitWithStdin, "exit-with-stdin", false, "stop when stdin closes: a private service, for the client that started it")
	cmd.Flags().StringVar(&o.Run.Model, "model", "", "a private service's model for every workspace")
	cmd.Flags().StringVar(&o.Run.Agent, "agent", "", "a private service's agent")
	cmd.Flags().StringVar(&o.Run.Agency, "agency", "", "a private service's agency level")
	cmd.Flags().StringArrayVar(&o.Run.PluginDirs, "plugin-dir", nil, "a private service's extra plugin folder (repeatable)")
	cmd.Flags().StringArrayVar(&o.Run.AddDirs, "add-dir", nil, "a private service's extra read-write folder (repeatable)")
	cmd.Flags().StringVar(&o.Run.SessionDir, "session-dir", "", "a private service's session folder")
	cmd.Flags().BoolVar(&o.Run.TrustProject, "trust-project", false, "a private service trusts project settings")
	cmd.Flags().StringVar(&promptFile, "append-system-prompt-file", "", "a private service adds this file to the agent's instructions")
	for _, f := range []string{"exit-with-stdin", "model", "agent", "agency", "plugin-dir", "add-dir", "session-dir", "trust-project", "append-system-prompt-file"} {
		_ = cmd.Flags().MarkHidden(f) // the CLI's, for the private service it starts
	}
	cmd.PreRunE = func(*cobra.Command, []string) error {
		if exitWithStdin { // a private service
			o.ExitWith, o.Private = os.Stdin, true
		}
		o.AdoptPath = !o.Private // a private service has its terminal's
		if promptFile != "" {
			data, err := os.ReadFile(promptFile)
			if err != nil {
				return fmt.Errorf("%w: --append-system-prompt-file: %v", errUsage, err)
			}
			o.Run.AppendSystemPrompt = strings.TrimSpace(string(data))
		}
		return nil
	}
	cmd.Flags().StringVar(&license, "license", "", "show the license and exit: the NOTICE, or =full (the Apache License) or =third-party (the notices of the software blitzd includes)")
	cmd.Flags().Lookup("license").NoOptDefVal = "notice"
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return fmt.Errorf("%w: %v", errUsage, err) })
	cmd.AddCommand(newDoctorCommand())
	cmd.SetArgs(args)
	if err := cmd.ExecuteContext(ctx); err != nil {
		if errors.Is(err, errChecksFailed) {
			return exitFailure // the report says which
		}
		fmt.Fprintln(os.Stderr, "blitzd:", err)
		if errors.Is(err, socket.ErrRunning) || errors.Is(err, errUsage) {
			return exitUsage
		}
		return exitFailure
	}
	return 0
}

// errChecksFailed is blitzd doctor's error when a check failed.
var errChecksFailed = errors.New("checks failed")

// newDoctorCommand is blitzd doctor: the setup's checks, which blitz
// doctor runs here so the CLI needn't carry the engine.
func newDoctorCommand() *cobra.Command {
	var o doctor.Options
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check configuration, credentials, sandbox and integrations (blitz doctor)",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("%w: doctor takes no arguments", errUsage)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if failed := doctor.Print(cmd.OutOrStdout(), doctor.Run(cmd.Context(), o)); failed > 0 {
				return fmt.Errorf("%w: %d", errChecksFailed, failed)
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&o.Config, "config", "", "configuration file (default: the usual search)")
	f.StringVarP(&o.Dir, "dir", "d", "", "the workspace (default: the current directory)")
	f.StringVarP(&o.Model, "model", "m", "", "check this model instead of the configured one")
	f.StringVarP(&o.Agent, "agent", "a", "", "the agent")
	f.StringVar(&o.Agency, "agency", "", "the agency level")
	f.StringArrayVar(&o.PluginDirs, "plugin-dir", nil, "an extra plugin folder (repeatable)")
	f.StringArrayVar(&o.AddDirs, "add-dir", nil, "an extra read-write folder (repeatable)")
	f.BoolVar(&o.Online, "online", false, "also send a tiny request to the model and connect to MCP servers")
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return fmt.Errorf("%w: %v", errUsage, err) })
	return cmd
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
