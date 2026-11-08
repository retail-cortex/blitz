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

package tools

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"

	"github.com/retail-cortex/blitz/pkg/api"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// BlitzRunner runs one of Blitz's own commands for the blitz tool, its
// arguments checked against the catalog: what it answers, or why not
// (spec_060_blitz_tools DRV-01). The workspace supplies it.
type BlitzRunner func(ctx context.Context, c api.BlitzCommand, args map[string]any) (any, error)

// blitzTool is the blitz tool's state: the workspace's runner.
type blitzTool struct {
	mu  sync.Mutex
	run BlitzRunner
}

// SetBlitzRunner supplies the workspace's commands.
func (r *Registry) SetBlitzRunner(run BlitzRunner) {
	r.blitz.mu.Lock()
	defer r.blitz.mu.Unlock()
	r.blitz.run = run
}

// BlitzInput is what the blitz tool takes.
type BlitzInput struct {
	Command string         `json:"command" jsonschema:"The command, its group then its verb: connector save, settings show, open file, help"`
	Args    map[string]any `json:"args,omitempty" jsonschema:"Its arguments, by name, as help lists them"`
}

// BlitzOutput is what it returns.
type BlitzOutput struct {
	Command string `json:"command,omitempty"`
	Result  any    `json:"result,omitempty"`
	Error   string `json:"error,omitempty"`
	// Help is the command's arguments (after a call that got them
	// wrong), or the catalog (help, or an unknown command).
	Help string `json:"help,omitempty"`
}

// blitzDescription is the tool's description: the groups, a line each,
// and an example; help gives the rest (DRV-03).
func blitzDescription() string {
	var b strings.Builder
	b.WriteString("Run Blitz's own commands: set up and change Blitz itself (its connections to databases, settings, MCP servers, agents, skills, workflows, jobs), check it (doctor, the organization's policy), and open its views for the user (a file in the editor, Pictures, Studio, Settings). The same commands as the blitz CLI. Use this, never the shell's blitz, Blitz's files or its binary, to learn or change how Blitz is set up. Commands that change something ask the user themselves: don't ask first as well. Secrets are never arguments: the user is asked for them. Call help (or help with a group or command) for the arguments.\nGroups: ")
	var groups []string
	for _, g := range api.BlitzGroups() {
		var verbs []string
		for _, c := range g {
			if _, verb, ok := strings.Cut(c.Name, " "); ok {
				verbs = append(verbs, verb)
			}
		}
		s := g[0].Group()
		if len(verbs) > 0 {
			s += " (" + strings.Join(verbs, ", ") + ")"
		}
		groups = append(groups, s)
	}
	b.WriteString(strings.Join(groups, "; "))
	b.WriteString(".\nExample, a SQLite database in the workspace: {\"command\": \"connector save\", \"args\": {\"kind\": \"sqlite\", \"name\": \"recipes\", \"description\": \"The recipes and their ingredients\", \"settings\": {\"database\": \"recipes.db\"}}}.")
	return b.String()
}

// NewBlitzTool is the blitz tool (spec_060_blitz_tools DRV-01..08): one
// tool for Blitz's commands, checked against the catalog, the rules
// applied, and run by the workspace.
func NewBlitzTool(r *Registry) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{Name: "blitz", Description: blitzDescription()},
		func(ctx agent.Context, in BlitzInput) (BlitzOutput, error) {
			// Jobs are today's create_job and list_jobs, which stay as
			// tools of their own (DRV-42).
			if name := strings.Join(strings.Fields(strings.ToLower(in.Command)), " "); name == "job create" || name == "job list" {
				return r.runJobCommand(ctx, name, in.Args), nil
			}
			return r.runBlitz(ctx, in), nil
		})
}

// runJobCommand runs job create or job list as the tools they are.
func (r *Registry) runJobCommand(ctx agent.Context, name string, args map[string]any) BlitzOutput {
	c, _ := api.FindBlitzCommand(name)
	checked, err := CheckBlitzArgs(c, args)
	if err != nil {
		return BlitzOutput{Command: name, Error: err.Error(), Help: c.Usage()}
	}
	t, ok := r.tools[map[string]string{"job create": "create_job", "job list": "list_jobs"}[name]].(interface {
		Run(agent.Context, any) (map[string]any, error)
	})
	if !ok {
		return BlitzOutput{Command: name, Error: "jobs aren't available here"}
	}
	out, err := t.Run(ctx, checked)
	if err != nil {
		return BlitzOutput{Command: name, Error: err.Error()}
	}
	if e, _ := out["error"].(string); e != "" {
		return BlitzOutput{Command: name, Error: e}
	}
	return BlitzOutput{Command: name, Result: out}
}

// runBlitz checks in against the catalog and runs it.
func (r *Registry) runBlitz(ctx context.Context, in BlitzInput) BlitzOutput {
	name := strings.Join(strings.Fields(strings.ToLower(in.Command)), " ")
	if name == "" || name == "help" {
		about, _ := in.Args["command"].(string)
		return BlitzOutput{Command: "help", Help: BlitzHelp(about)}
	}
	c, ok := api.FindBlitzCommand(name)
	if !ok {
		return BlitzOutput{Command: name, Error: fmt.Sprintf("there's no command %q", name), Help: BlitzHelp(strings.Fields(name)[0])}
	}
	args, err := CheckBlitzArgs(c, in.Args)
	if err != nil {
		return BlitzOutput{Command: c.Name, Error: err.Error(), Help: c.Usage()}
	}
	// A job runs only the commands its permissions name, reading ones too
	// (DRV-70): asked of its permissions, as any of its actions.
	if _, unattended := ctx.Value(unattendedKey{}).(api.Approver); unattended {
		if err := r.hooks.Approve(ctx, api.ApprovalRequest{Tool: "blitz:" + c.Name, Kind: api.ActionBlitz, Detail: "blitz " + c.Name, Targets: []string{c.Name}}); err != nil {
			return BlitzOutput{Command: c.Name, Error: err.Error()}
		}
	}
	r.blitz.mu.Lock()
	run := r.blitz.run
	r.blitz.mu.Unlock()
	if run == nil {
		return BlitzOutput{Command: c.Name, Error: "Blitz's commands aren't available here"}
	}
	out, err := run(ctx, c, args)
	if err != nil {
		return BlitzOutput{Command: c.Name, Error: err.Error()}
	}
	return BlitzOutput{Command: c.Name, Result: out}
}

// BlitzHelp is the catalog's help: every group's commands, a line each;
// for a group, its commands with their arguments; for a command, its
// arguments (DRV-03).
func BlitzHelp(about string) string {
	about = strings.Join(strings.Fields(strings.ToLower(about)), " ")
	if c, ok := api.FindBlitzCommand(about); ok {
		return c.Usage()
	}
	var b strings.Builder
	for _, g := range api.BlitzGroups() {
		if about != "" && g[0].Group() != about {
			continue
		}
		for _, c := range g {
			if about != "" {
				b.WriteString(c.Usage() + "\n")
			} else {
				fmt.Fprintf(&b, "%s: %s\n", c.Name, c.Summary)
			}
		}
	}
	if b.Len() == 0 {
		return BlitzHelp("")
	}
	if about == "" {
		b.WriteString("help <command> gives a command's arguments.")
	}
	return strings.TrimSpace(b.String())
}

// CheckBlitzArgs checks args against c's: the required ones there, each
// of its type and among its values, none unknown. Integers come as JSON
// numbers; they're given back as int.
func CheckBlitzArgs(c api.BlitzCommand, args map[string]any) (map[string]any, error) {
	out := map[string]any{}
	var errs []error
	for k := range args {
		if !slices.ContainsFunc(c.Args, func(a api.CommandArg) bool { return a.Name == k }) {
			errs = append(errs, fmt.Errorf("%s takes no argument %q", c.Name, k))
		}
	}
	for _, a := range c.Args {
		v, ok := args[a.Name]
		if !ok || v == nil || v == "" {
			if a.Required {
				errs = append(errs, fmt.Errorf("%s is required", a.Name))
			}
			continue
		}
		got, err := argOf(a, v)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out[a.Name] = got
	}
	return out, errors.Join(errs...)
}

// argOf is v as a's type.
func argOf(a api.CommandArg, v any) (any, error) {
	switch a.Type {
	case "string":
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("%s must be text", a.Name)
		}
		if len(a.Values) > 0 && !slices.Contains(a.Values, s) {
			return nil, fmt.Errorf("%s must be one of %s", a.Name, strings.Join(a.Values, ", "))
		}
		return s, nil
	case "integer":
		switch n := v.(type) {
		case float64:
			if n != math.Trunc(n) {
				return nil, fmt.Errorf("%s must be a whole number", a.Name)
			}
			return int(n), nil
		case int:
			return n, nil
		}
		return nil, fmt.Errorf("%s must be a number", a.Name)
	case "boolean":
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("%s must be true or false", a.Name)
		}
		return b, nil
	case "object":
		m, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s must be a JSON object", a.Name)
		}
		return m, nil
	case "list":
		l, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("%s must be a list", a.Name)
		}
		return l, nil
	}
	return v, nil
}
