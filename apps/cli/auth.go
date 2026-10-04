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

package main

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"text/tabwriter"

	"github.com/retail-cortex/blitz/pkg/client"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/spf13/cobra"
)

// newAuthCommand is `blitz auth status|login|logout`: signing in to model
// providers with their vendors' tools, run by the Blitz service so the
// credentials land where it reads them. Signing in adds to API keys
// (blitz config keys), never replaces them.
func newAuthCommand(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Sign in to model providers (Google, Anthropic, AWS, Azure) with their own tools",
		Long: `Signs in to a model provider with its vendor's tool, on the Blitz service's
machine: gcloud (Google: Gemini and Claude on Vertex AI), ant (an Anthropic
account), aws (Amazon Bedrock, IAM Identity Center) or az (Azure, Entra ID).
The tool opens the sign-in page in the browser. API keys keep working:
signing in is another way, chosen with blitz config set-auth.`,
	}
	var profile string
	settings := func(cmd *cobra.Command) (*client.Settings, string, error) {
		cfg, err := loadConfig(g)
		if err != nil {
			return nil, "", err
		}
		dir, err := filepath.Abs(config.ExpandHome(cfg.Tools.WorkspaceDir))
		if err != nil {
			return nil, "", err
		}
		sock, err := ensureService(cmd.Context())
		if err != nil {
			return nil, "", err
		}
		return client.AttachSettings(sock), dir, nil
	}
	status := &cobra.Command{
		Use:   "status",
		Short: "Show each provider's sign-in",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, dir, err := settings(cmd)
			if err != nil {
				return err
			}
			list, err := s.SignIns(cmd.Context(), dir, "", profile)
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 2, 4, 2, ' ', 0)
			for _, st := range list {
				fmt.Fprintf(tw, "%s\t%s\n", st.Provider, describeSignIn(st))
			}
			return tw.Flush()
		},
	}
	login := &cobra.Command{
		Use:   "login <google|anthropic|aws|azure>",
		Short: "Sign in to a provider: its tool opens the sign-in page",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, dir, err := settings(cmd)
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt) // Ctrl+C stops the sign-in
			defer stop()
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Signing in to %s: finish in the browser it opens (Ctrl+C stops).\n", args[0])
			st, err := s.SignIn(ctx, dir, args[0], profile, func(e client.SignInEvent) {
				switch {
				case e.Code != "":
					fmt.Fprintf(out, "  %s\n  Code: %s\n", e.Line, e.Code)
				case e.URL != "":
					fmt.Fprintf(out, "  If the browser didn't open: %s\n", e.URL)
				default:
					fmt.Fprintf(out, "  %s\n", e.Line)
				}
			})
			if err != nil {
				return withCode(signInExit(err), err)
			}
			fmt.Fprintf(out, "✓ %s: %s\n", st.Provider, describeSignIn(st))
			return nil
		},
	}
	logout := &cobra.Command{
		Use:   "logout <google|anthropic|aws|azure>",
		Short: "Sign out of a provider with its tool",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, dir, err := settings(cmd)
			if err != nil {
				return err
			}
			st, err := s.SignOut(cmd.Context(), dir, args[0], profile)
			if err != nil {
				return withCode(signInExit(err), err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", st.Provider, describeSignIn(st))
			return nil
		},
	}
	for _, c := range []*cobra.Command{status, login, logout} {
		c.Flags().StringVar(&profile, "profile", "", "The profile to use (anthropic, aws), over the configured one")
	}
	cmd.AddCommand(status, login, logout)
	return cmd
}

// describeSignIn says how a provider's sign-in stands.
func describeSignIn(s client.SignInStatus) string {
	switch {
	case s.SignedIn:
		return "signed in: " + s.Detail
	case !s.ToolFound:
		return fmt.Sprintf("not signed in (%s isn't installed: %s)", s.Tool, s.Install)
	default:
		return fmt.Sprintf("not signed in (blitz auth login %s): %s", s.Provider, s.Detail)
	}
}

// signInExit is a failed sign-in's exit code: a usage error for what
// can't work (an unknown provider, a bad profile, no tool).
func signInExit(err error) int {
	if errors.Is(err, client.ErrUnknownProvider) || errors.Is(err, client.ErrInvalidProfile) || errors.Is(err, client.ErrNoTool) {
		return exitUsage
	}
	return exitFailure
}
