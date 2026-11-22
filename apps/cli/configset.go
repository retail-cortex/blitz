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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"

	"github.com/retail-cortex/blitz/pkg/client"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/socket"
)

// configValueCommands are blitz config get, set, edit, check and
// reference: every setting from the terminal (spec_067_cli_parity
// CLP-40).
func configValueCommands(g *globalFlags) []*cobra.Command {
	var workspace, asJSON bool
	keys := func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		var out []string
		for _, s := range config.Reference() {
			if s.Type != "table" {
				out = append(out, s.Key)
			}
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
	get := &cobra.Command{
		Use:               "get <key>",
		Short:             "Print one setting's value, as it's in effect here (secrets masked)",
		Example:           "  blitz config get ui.theme\n  blitz config get llm --json",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: keys,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(g)
			if err != nil {
				return err
			}
			v, ok, err := settingValue(maskConfig(cfg), args[0])
			if err != nil {
				return err
			}
			if !ok {
				return withCode(exitUsage, fmt.Errorf("no setting is %q (blitz config reference lists them)", args[0]))
			}
			return printValue(cmd, v, asJSON)
		},
	}
	get.Flags().BoolVar(&asJSON, "json", false, "Print JSON")

	set := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Change one setting, for every workspace or (-w) this one; \"\" puts it back to the default",
		Long: "Sets one of the settings the desktop app's forms set (blitz config reference says which), in the global settings " +
			"or, with -w, this workspace's own, and applies it to the open workspaces when the service runs. Edit the file for the rest: blitz config edit.",
		Example:           "  blitz config set ui.theme dracula\n  blitz config set -w llm.gemini.model gemini-flash\n  blitz config set search.enabled true",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: keys,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := ""
			if workspace {
				d, err := workspaceDir(g)
				if err != nil {
					return err
				}
				dir = d
			}
			path, problems, err := setValue(cmd.Context(), g, dir, args[0], args[1])
			if err != nil {
				return withCode(exitUsage, err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), i18n.T("config.set", "key", args[0], "value", args[1], "path", path))
			for _, p := range problems {
				fmt.Fprintln(cmd.ErrOrStderr(), "! "+p)
			}
			return nil
		},
	}
	set.Flags().BoolVarP(&workspace, "workspace", "w", false, "This workspace's own setting (--dir, else the current folder)")

	edit := &cobra.Command{
		Use:   "edit",
		Short: "Open the settings file in $EDITOR, and check it when you save",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path := filepath.Join(config.ConfigDir(g.config), ".env.toml")
			if workspace {
				d, err := workspaceDir(g)
				if err != nil {
					return err
				}
				path = filepath.Join(config.WorkspaceSettingsDir(g.config, d), ".env.toml")
			}
			return editSettings(cmd, path)
		},
	}
	edit.Flags().BoolVarP(&workspace, "workspace", "w", false, "This workspace's own settings file")

	check := &cobra.Command{
		Use:   "check [file]",
		Short: "Check a settings file (default: the global one) and say what's wrong, by line",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := filepath.Join(config.ConfigDir(g.config), ".env.toml")
			if len(args) == 1 {
				path = args[0]
			}
			text, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if n := printProblems(cmd, path, string(text)); n > 0 {
				return withCode(exitFailure, fmt.Errorf("%d problems in %s", n, path))
			}
			fmt.Fprintln(cmd.OutOrStdout(), i18n.T("config.check_ok", "path", path))
			return nil
		},
	}

	reference := &cobra.Command{
		Use:               "reference [key|section]",
		Short:             "What each setting does: its type and default",
		Example:           "  blitz config reference ui\n  blitz config reference sandbox.allow_network",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: keys,
		RunE: func(cmd *cobra.Command, args []string) error {
			prefix := ""
			if len(args) == 1 {
				prefix = strings.TrimSuffix(args[0], ".")
			}
			var rows []config.Setting
			for _, s := range config.Reference() {
				if prefix == "" || s.Key == prefix || strings.HasPrefix(s.Key, prefix+".") {
					rows = append(rows, s)
				}
			}
			if len(rows) == 0 {
				return withCode(exitUsage, fmt.Errorf("no setting is %q", prefix))
			}
			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(rows)
			}
			out := cmd.OutOrStdout()
			for _, s := range rows {
				def := ""
				if s.Default != "" {
					def = " = " + s.Default
				}
				settable := ""
				if config.Settable(s.Key) {
					settable = " (config set)"
				}
				fmt.Fprintf(out, "%s  %s%s%s\n", s.Key, s.Type, def, settable)
				if s.Doc != "" {
					fmt.Fprintf(out, "    %s\n", strings.ReplaceAll(strings.TrimSpace(s.Doc), "\n", "\n    "))
				}
			}
			return nil
		},
	}
	reference.Flags().BoolVar(&asJSON, "json", false, "Print JSON")
	return []*cobra.Command{get, set, edit, check, reference}
}

// setValue sets key through the running service, so the open workspaces
// take it up at once, or in the file when none runs, or when --config
// names settings of the run's own (the service reads the default ones).
var setValue = func(ctx context.Context, g *globalFlags, dir, key, value string) (string, []string, error) {
	if sock := socket.DefaultSocket(); g.config == "" && socket.Running(sock) {
		c, err := client.AttachSettings(sock).SetConfigValue(ctx, dir, key, value)
		if err == nil {
			return c.Path, c.Problems, nil
		}
		return "", nil, errors.New(errorText(err, false))
	}
	path, err := config.SetValue(g.config, dir, key, value)
	return path, nil, err
}

// settingValue is the value at a dotted key of cfg (a table as a whole),
// false when there's none.
func settingValue(cfg config.Config, key string) (any, bool, error) {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		return nil, false, err
	}
	var m map[string]any
	if _, err := toml.Decode(buf.String(), &m); err != nil {
		return nil, false, err
	}
	var cur any = m
	for _, part := range strings.Split(key, ".") {
		t, ok := cur.(map[string]any)
		if !ok {
			return nil, false, nil
		}
		if cur, ok = t[part]; !ok {
			return nil, false, nil
		}
	}
	return cur, true, nil
}

// printValue prints a setting's value: a table as TOML, else the value.
func printValue(cmd *cobra.Command, v any, asJSON bool) error {
	out := cmd.OutOrStdout()
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	switch t := v.(type) {
	case map[string]any:
		return toml.NewEncoder(out).Encode(t)
	case []any:
		parts := make([]string, len(t))
		for i, x := range t {
			parts[i] = fmt.Sprint(x)
		}
		fmt.Fprintln(out, strings.Join(parts, ", "))
	default:
		fmt.Fprintln(out, v)
	}
	return nil
}

// printProblems prints what's wrong with a settings file's text, by line
// (errors, then warnings that don't stop it loading); how many errors.
func printProblems(cmd *cobra.Command, path, text string) int {
	problems := config.CheckSettings(text)
	sort.SliceStable(problems, func(i, j int) bool { return problems[i].Line < problems[j].Line })
	errs := 0
	for _, p := range problems {
		kind := i18n.T("config.warning")
		if p.Error {
			kind = i18n.T("config.error")
			errs++
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "%s:%d:%d: %s: %s\n", path, p.Line, p.Col, kind, p.Message)
	}
	return errs
}

// editSettings opens path in $VISUAL or $EDITOR and checks it once saved:
// with problems, it offers to open it again (on a terminal), else says
// what they are.
func editSettings(cmd *cobra.Command, path string) error {
	editor := strings.TrimSpace(os.Getenv("VISUAL"))
	if editor == "" {
		editor = strings.TrimSpace(os.Getenv("EDITOR"))
	}
	if editor == "" {
		return withCode(exitUsage, fmt.Errorf("set $EDITOR (or $VISUAL) to edit %s", path))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			return err
		}
	}
	for {
		c := exec.CommandContext(cmd.Context(), "sh", "-c", editor+` "$1"`, "sh", path)
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := c.Run(); err != nil {
			return err
		}
		text, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		n := printProblems(cmd, path, string(text))
		if n == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), i18n.T("config.check_ok", "path", path))
			return nil
		}
		if !stdoutIsTerminal() || !editAgain(cmd) {
			return withCode(exitFailure, fmt.Errorf("%d problems in %s", n, path))
		}
	}
}

// editAgain asks whether to open the file again to fix its problems.
func editAgain(cmd *cobra.Command) bool {
	fmt.Fprint(cmd.OutOrStdout(), i18n.T("config.edit_again")+" [y/N] ")
	line, _ := readLine(cmd.InOrStdin())
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes", "s", "si", "sí", "o", "oui":
		return true
	}
	return false
}
