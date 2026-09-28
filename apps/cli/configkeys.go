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
	"bufio"
	"cmp"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// keyCommands manage providers' API keys: kept in the OS keychain, the
// settings file referring to them. --workspace works on the workspace's
// own settings (--dir, else the current directory) instead of the global
// ones.
func keyCommands(g *globalFlags) []*cobra.Command {
	var workspace bool
	scope := func() (string, error) {
		if !workspace {
			return "", nil
		}
		cfg, err := loadConfig(g)
		if err != nil {
			return "", err
		}
		return cfg.Tools.WorkspaceDir, nil
	}
	done := func(cmd *cobra.Command, path string) {
		fmt.Fprintf(cmd.OutOrStdout(), "Updated %s. Workspaces open in the service use it when they reopen (the desktop app's settings apply at once).\n", path)
	}
	provider := func(args []string) (string, error) {
		p := strings.ToLower(args[0])
		for _, k := range config.KeyedProviders {
			if k == p {
				return p, nil
			}
		}
		return "", withCode(exitUsage, fmt.Errorf("%q takes no API key (one of %s)", args[0], strings.Join(config.KeyedProviders, ", ")))
	}

	keys := &cobra.Command{
		Use:   "keys",
		Short: "Show each provider's API key source (never the key)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := scope()
			if err != nil {
				return err
			}
			info, err := config.Describe(g.config, dir)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "settings: %s\nkeys kept in: %s\n", info.Path, info.SecretStore)
			for _, p := range info.Providers {
				src := string(p.KeySource)
				if p.KeyMissing {
					src += " (missing from the store: set it again)"
				}
				if p.KeySource == config.KeyPlain || p.KeySource == config.KeyObfuscated {
					src += " (move it: blitz config secure-key " + p.Name + ")"
				}
				switch p.Auth {
				case config.AuthADC:
					src = "Google Cloud ADC, project " + cmp.Or(p.ProjectID, "from GOOGLE_CLOUD_PROJECT") + ", location " + cmp.Or(p.Location, "from GOOGLE_CLOUD_LOCATION, else global") + " (key: " + src + ", unused)"
				case config.AuthOAuth:
					src = "OAuth, `ant auth login` profile " + cmp.Or(p.Profile, "ant's active one") + " (key: " + src + ", unused)"
				}
				fmt.Fprintf(out, "  %-10s %s\n", p.Name, src)
			}
			return nil
		},
	}
	setKey := &cobra.Command{
		Use:   "set-key <provider>",
		Short: "Store a provider's API key in the OS keychain (read from stdin)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := provider(args)
			if err != nil {
				return err
			}
			dir, err := scope()
			if err != nil {
				return err
			}
			key, err := readSecret(cmd, fmt.Sprintf("%s API key: ", p))
			if err != nil {
				return err
			}
			path, err := config.SetAPIKey(g.config, dir, p, key)
			if err != nil {
				return err
			}
			done(cmd, path)
			return nil
		},
	}
	removeKey := &cobra.Command{
		Use:   "remove-key <provider>",
		Short: "Remove a provider's API key (a workspace then uses the global one)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := provider(args)
			if err != nil {
				return err
			}
			dir, err := scope()
			if err != nil {
				return err
			}
			path, err := config.RemoveAPIKey(g.config, dir, p)
			if err != nil {
				return err
			}
			done(cmd, path)
			return nil
		},
	}
	secureKey := &cobra.Command{
		Use:   "secure-key <provider>",
		Short: "Move a key written in the settings file into the OS keychain",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := provider(args)
			if err != nil {
				return err
			}
			dir, err := scope()
			if err != nil {
				return err
			}
			path, err := config.SecureAPIKey(g.config, dir, p)
			if err != nil {
				return err
			}
			done(cmd, path)
			return nil
		},
	}
	var auth config.ProviderAuth
	setAuth := &cobra.Command{
		Use:   "set-auth <provider> <api_key|adc|oauth>",
		Short: "Choose how a provider signs in: an API key, Google Cloud ADC (gemini, anthropic) or an ant OAuth profile (anthropic)",
		Long: `Choose how a provider signs in.

  api_key  the provider's API key (the default; blitz config set-key)
  adc      gemini or anthropic (Claude) on Vertex AI with Google Cloud's
           Application Default Credentials (gcloud auth application-default
           login): --project and --location, else GOOGLE_CLOUD_PROJECT and
           GOOGLE_CLOUD_LOCATION (default global)
  oauth    anthropic with an Anthropic Console sign-in (ant auth login):
           --profile, else ant's active profile`,
		Example: `  blitz config set-auth gemini adc --project my-project
  blitz config set-auth anthropic adc --project my-project --location us-east5
  blitz config set-auth anthropic oauth --profile work
  blitz config set-auth gemini api_key`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := provider(args)
			if err != nil {
				return err
			}
			dir, err := scope()
			if err != nil {
				return err
			}
			auth.Method = strings.ToLower(args[1])
			path, err := config.SetAuth(g.config, dir, p, auth)
			if err != nil {
				return withCode(exitUsage, err)
			}
			done(cmd, path)
			return nil
		},
	}
	setAuth.Flags().StringVar(&auth.ProjectID, "project", "", "adc: the Google Cloud project")
	setAuth.Flags().StringVar(&auth.Location, "location", "", "adc: the Vertex AI location (global, us-central1, …)")
	setAuth.Flags().StringVar(&auth.Profile, "profile", "", "oauth: the ant auth login profile")
	all := []*cobra.Command{keys, setKey, removeKey, secureKey, setAuth}
	for _, c := range all {
		c.Flags().BoolVarP(&workspace, "workspace", "w", false, "The workspace's own settings (--dir, else the current directory)")
	}
	return all
}

// readSecret reads one line: without echo from a terminal, else from
// stdin as piped. A key never goes in arguments, where ps and the shell's
// history would see it.
func readSecret(cmd *cobra.Command, prompt string) (string, error) {
	in := cmd.InOrStdin()
	if f, ok := in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Fprint(cmd.ErrOrStderr(), prompt)
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(cmd.ErrOrStderr())
		return strings.TrimSpace(string(b)), err
	}
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	if line = strings.TrimSpace(line); line == "" {
		return "", withCode(exitUsage, fmt.Errorf("no key on stdin"))
	}
	return line, nil
}
