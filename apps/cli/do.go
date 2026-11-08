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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// errNotConfirmed is blitz do's answer when the person says no.
var errNotConfirmed = errors.New("nothing was changed")

// newDoCommand is `blitz do <command> [name=value…]`: one of Blitz's own
// commands, the same catalog as the agent's blitz tool (spec_060_blitz_tools
// DRV-02, DRV-74). One that changes something says what it'll do and asks
// first (--yes doesn't), and asks for any secret hidden.
func newDoCommand(g *globalFlags) *cobra.Command {
	var asJSON, yes bool
	var argsJSON string
	cmd := &cobra.Command{
		Use:   "do <command> [name=value…]",
		Short: "Run one of Blitz's own commands (connector save, settings set, doctor, …); blitz do help lists them",
		Long: `Runs one of Blitz's own commands, the same ones the agent's blitz tool runs:
  blitz do help                       every command
  blitz do help connector save        one command's arguments
  blitz do connector list
  blitz do connector save kind=sqlite name=recipes settings='{"database":"recipes.db"}'
  blitz do settings set key=mode value=default
A value is read as JSON when it is (true, 3, {"a":1}, ["x"]), else as text;
--args takes them all as one JSON object. A command that changes something
shows what it'll do and asks (--yes doesn't); a secret it needs is typed
hidden. --json prints the result as JSON.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, words []string) error {
			name, args, err := doArgs(words, argsJSON)
			if err != nil {
				return withCode(exitUsage, err)
			}
			b, _ := json.Marshal(args)
			r, err := attachWorkspace(cmd.Context(), g)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			res, err := r.RunBlitzCommand(cmd.Context(), name, string(b), false, nil)
			if err != nil {
				return err
			}
			if res.Confirm != "" {
				fmt.Fprintln(out, res.Confirm)
				if res.Diff != "" {
					fmt.Fprintln(out, res.Diff)
				}
				if !yes && !confirmed(cmd.InOrStdin(), out) {
					return withCode(exitFailure, errNotConfirmed)
				}
				secrets := map[string]string{}
				for _, k := range res.Asks {
					v, err := readHidden(cmd.InOrStdin(), out, k)
					if err != nil {
						return err
					}
					secrets[k] = v
				}
				if res, err = r.RunBlitzCommand(cmd.Context(), name, string(b), true, secrets); err != nil {
					return err
				}
			}
			switch {
			case res.Help != "":
				fmt.Fprintln(out, res.Help)
			case asJSON:
				fmt.Fprintln(out, res.ResultJson)
			default:
				var pretty bytes.Buffer
				if json.Indent(&pretty, []byte(res.ResultJson), "", "  ") == nil {
					fmt.Fprintln(out, pretty.String())
				} else {
					fmt.Fprintln(out, res.ResultJson)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print the result as one line of JSON")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Don't ask before a change (secrets are still asked)")
	cmd.Flags().StringVar(&argsJSON, "args", "", "The arguments as one JSON object")
	return cmd
}

// doArgs splits blitz do's words into the command (the words before the
// first name=value) and its arguments; with help, the rest is what to
// help with.
func doArgs(words []string, argsJSON string) (string, map[string]any, error) {
	args := map[string]any{}
	if strings.TrimSpace(argsJSON) != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return "", nil, fmt.Errorf("--args: %w", err)
		}
	}
	var name []string
	for i, w := range words {
		k, v, ok := strings.Cut(w, "=")
		if !ok {
			if len(args) > 0 && len(name) > 0 && i > len(name) {
				return "", nil, fmt.Errorf("%q: arguments are name=value", w)
			}
			name = append(name, w)
			continue
		}
		var parsed any
		if err := json.Unmarshal([]byte(v), &parsed); err != nil {
			parsed = v
		}
		args[k] = parsed
	}
	if len(name) > 1 && strings.EqualFold(name[0], "help") {
		return "help", map[string]any{"command": strings.Join(name[1:], " ")}, nil
	}
	return strings.Join(name, " "), args, nil
}

// confirmed asks y/N on out, reading the answer from in.
func confirmed(in io.Reader, out io.Writer) bool {
	fmt.Fprint(out, i18n.T("do.confirm"))
	line, _ := readLine(in)
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes", "s", "si", "sí", "o", "oui": // the prompt's languages
		return true
	}
	return false
}

// readHidden asks for a secret's value: hidden on a terminal, else read
// as a line (a pipe, in scripts).
func readHidden(in io.Reader, out io.Writer, what string) (string, error) {
	fmt.Fprint(out, i18n.T("do.secret", "what", what))
	if f, ok := in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(out)
		return strings.TrimSpace(string(b)), err
	}
	line, err := readLine(in)
	if err != nil && line == "" {
		return "", fmt.Errorf("%s: %w", what, err)
	}
	return strings.TrimSpace(line), nil
}

// readLine reads one line from in, a byte at a time, so nothing after it
// is read ahead: the confirmation and the secrets after it come from the
// same input (a pipe, in scripts).
func readLine(in io.Reader) (string, error) {
	var b strings.Builder
	c := make([]byte, 1)
	for {
		n, err := in.Read(c)
		if n == 1 {
			if c[0] == '\n' {
				return b.String(), nil
			}
			b.WriteByte(c[0])
		}
		if err != nil {
			return b.String(), err
		}
	}
}
