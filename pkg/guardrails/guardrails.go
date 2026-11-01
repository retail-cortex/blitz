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

// Package guardrails runs Blitz's guardrails on a model's request and
// reply, for a program that has those rather than an engine turn: a
// gateway between Blitz and the models (spec_053_enterprise EN-23). The
// rules and their checks are the engine's own (pkg/engine/gates), so a
// guardrail means the same there as on the desktop:
//
//   - a tool's result going back to the model: the injection guardrail
//     takes out instructions aimed at the agent (confirmed by the judge
//     when it asks), and custom tool-result guardrails are judged;
//   - what the client sends: the secrets guardrail masks or refuses
//     credentials;
//   - a tool call the model makes: the limits guardrail's domains and
//     commands, and custom action guardrails, judged;
//   - the model's text as it streams: the secrets guardrail, over a
//     window held back so a secret split between pieces is still caught.
//
// Custom output guardrails judge a whole answer, which a gateway has only
// once it's been sent: they aren't run here.
package guardrails

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/retail-cortex/blitz/pkg/engine/gates"
	"github.com/retail-cortex/blitz/pkg/redact"
)

// The engine's types, for programs outside it.
type (
	// Guardrail is one guardrail: a built-in one or a custom check.
	Guardrail = gates.Guardrail
	// Limits are what the limits guardrail holds a turn to.
	Limits = gates.Limits
	// Asker asks the judge's model: an instruction and a prompt, its
	// reply.
	Asker = gates.Asker
)

// Actions and stages, as the engine names them.
const (
	Block           = gates.Block
	Warn            = gates.Warn
	Redact          = gates.Redact
	StageToolResult = gates.StageToolResult
	StageOutput     = gates.StageOutput
	StageAction     = gates.StageAction
	Injection       = gates.BuiltinInjection
	Secrets         = gates.BuiltinSecrets
	LimitsID        = gates.BuiltinLimits
)

// Defaults are the built-in guardrails as they run with no files: on,
// each with its default action.
func Defaults() []Guardrail {
	out := make([]Guardrail, 0, len(gates.Builtins))
	for _, b := range gates.Builtins {
		out = append(out, gates.BuiltinDefault(b))
	}
	return out
}

// Load reads the guardrail files (*.md) in dirs, an earlier folder's
// winning, with each built-in one that has no file at its defaults.
func Load(dirs ...string) []Guardrail { return gates.LoadGuardrails(dirs...) }

// Parse reads one guardrail file's contents; name is its file's name
// without .md.
func Parse(name string, data []byte) Guardrail { return gates.ParseGuardrail(name, "", data) }

// Finding is what a guardrail did: for the log of what acted.
type Finding struct {
	Guardrail string `json:"guardrail"`
	Action    string `json:"action"` // block, warn, redact or pass
	Reason    string `json:"reason,omitempty"`
}

// Set is the guardrails one request runs under.
type Set struct {
	All   []Guardrail
	Agent string // the agent the request is for ("" for none)
	// Ask is the judge, for custom guardrails and the injection
	// guardrail's confirmation. Nil: patterns only; a custom guardrail
	// that blocks then refuses, as it can't be judged.
	Ask Asker
	// Redactor masks known values as well as the built-in patterns (nil:
	// the patterns and remembered values).
	Redactor *redact.Redactor
}

func (s Set) active(stage string) []Guardrail { return gates.Active(s.All, stage, s.Agent) }

// mask is text with the secrets guardrail g's masks, and whether it
// masked anything.
func (s Set) mask(g Guardrail, text string) (string, bool) {
	masked := s.Redactor.String(text)
	if g.PII {
		masked, _ = redact.PII(masked)
	}
	return masked, masked != text
}

// Input checks text the client sends to the model with the secrets
// guardrail: masked when it redacts, refused when it blocks.
func (s Set) Input(text string) (string, bool, []Finding) {
	var fs []Finding
	for _, g := range s.active(StageOutput) {
		if g.Builtin != Secrets {
			continue
		}
		masked, found := s.mask(g, text)
		if !found {
			continue
		}
		switch g.Action {
		case Block:
			return "", true, append(fs, Finding{g.Name, Block, "a secret in what was sent"})
		case Redact:
			text = masked
			fs = append(fs, Finding{g.Name, Redact, "a secret in what was sent"})
		default:
			fs = append(fs, Finding{g.Name, Warn, "a secret in what was sent"})
		}
	}
	return text, false, fs
}

// ToolResult checks a tool's result going back to the model. external is
// whether the tool brings in outside content (the web, a browser, an MCP
// server), which the injection guardrail checks. It returns the result
// to send (instructions aimed at the agent taken out), or withheld with
// the reason to send instead.
func (s Set) ToolResult(ctx context.Context, tool string, external bool, text string) (out string, withheld bool, fs []Finding) {
	out = text
	for _, g := range s.active(StageToolResult) {
		if !g.Checks(tool, external) {
			continue
		}
		var reason string
		if g.Builtin == Injection {
			phrase := gates.Injection(out)
			if phrase == "" {
				continue
			}
			if g.Judge && s.Ask != nil {
				if v, err := gates.ConfirmInjection(ctx, s.Ask, out); err == nil && v.Pass {
					fs = append(fs, Finding{g.Name, "pass", v.Reason})
					continue // quoted, not aimed at the agent
				}
			}
			reason = fmt.Sprintf("instructions aimed at the agent: %q", phrase)
		} else {
			pass, why := s.judge(ctx, g, out)
			if pass {
				continue
			}
			reason = why
		}
		switch {
		case g.Action == Redact && g.Builtin == Injection:
			out, _ = gates.RedactInjection(out)
			fs = append(fs, Finding{g.Name, Redact, reason})
		case g.Action == Block:
			fs = append(fs, Finding{g.Name, Block, reason})
			return fmt.Sprintf("[withheld by the %s guardrail: %s]", g.Name, reason), true, fs
		default:
			fs = append(fs, Finding{g.Name, Warn, reason})
		}
	}
	return out, false, fs
}

// judge is whether text passes custom guardrail g, and why not. One that
// can't be judged passes, unless it blocks.
func (s Set) judge(ctx context.Context, g Guardrail, text string) (bool, string) {
	if s.Ask == nil {
		return g.Action != Block, "it can't be judged here"
	}
	v, err := gates.JudgeGuardrail(ctx, s.Ask, g, s.Redactor.String(text))
	if err != nil {
		return g.Action != Block, "it couldn't be judged: " + err.Error()
	}
	return v.Pass, v.Reason
}

// ToolCall checks a tool call the model makes before the client sees it:
// the limits guardrail's domains (web_fetch, a browser's navigate) and
// commands (run_shell_command), and custom action guardrails. A refusal
// is what to tell the agent instead of the call.
func (s Set) ToolCall(ctx context.Context, tool string, args map[string]any, external bool) (refusal string, fs []Finding) {
	str := func(k string) string { v, _ := args[k].(string); return v }
	for _, g := range s.active(StageAction) {
		if !g.Checks(tool, external) {
			continue
		}
		var why string
		if g.Builtin == LimitsID {
			l := g.Limits
			switch {
			case tool == "web_fetch" || (tool == "browser" && str("action") == "navigate"):
				if u := str("url"); !gates.DomainAllowed(u, l.AllowedDomains) {
					why = fmt.Sprintf("%s isn't in the allowed domains (%s)", u, strings.Join(l.AllowedDomains, ", "))
				}
			case tool == "run_shell_command":
				if glob := gates.CommandDenied(str("command"), gates.ExpandCommands(l.Commands)); glob != "" {
					why = fmt.Sprintf("the command matches %q, which isn't allowed", glob)
				}
			}
		} else {
			data, _ := json.Marshal(args)
			if pass, reason := s.judge(ctx, g, fmt.Sprintf("Tool %s with arguments %s", tool, data)); !pass {
				why = reason
			}
		}
		if why == "" {
			continue
		}
		if g.Builtin == LimitsID || g.Action == Block {
			fs = append(fs, Finding{g.Name, Block, why})
			return fmt.Sprintf("The %s guardrail refused %s: %s.", g.Name, tool, why), fs
		}
		fs = append(fs, Finding{g.Name, Warn, why})
	}
	return "", fs
}
