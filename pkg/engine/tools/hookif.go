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
	"net/url"
	"strings"
	"sync"
)

// callTargets are what permission rules of each kind see in a call of tool
// with args: its name, and the command, path, host, skill or agent it
// names.
func callTargets(tool string, args map[string]any) map[string][]string {
	str := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := args[k].(string); ok && v != "" {
				return v
			}
		}
		return ""
	}
	out := map[string][]string{RuleTool: {tool}}
	add := func(kind, v string) {
		if v != "" {
			out[kind] = append(out[kind], v)
		}
	}
	switch tool {
	case "run_shell_command":
		add(RuleShell, str("command"))
	case "read_file", "list_files", "glob", "grep", "view_image", "view_document":
		add(RuleRead, str("path"))
	case "create_file", "replace_in_file", "edit", "delete_snippet", "apply_patch", "notebook_edit":
		add(RuleWrite, str("path"))
	case "generate_audio":
		add(RuleRead, str("text_path"))
		add(RuleWrite, str("path"))
	case "export_pdf": // path, or markdown given
		add(RuleRead, str("path"))
		add(RuleWrite, ExportPDFOutputPath(str("path"), str("output")))
	case "delete_file":
		add(RuleDelete, str("path"))
	case "web_fetch", "browser":
		if u, err := url.Parse(str("url")); err == nil {
			add(RuleWeb, strings.ToLower(u.Hostname()))
		}
	case "activate_skill", "run_skill_script":
		add(RuleSkill, str("skill_name", "skill", "name"))
	case "invoke_agent":
		add(RuleAgent, str("agent_name", "agent"))
	}
	return out
}

// matchesCall reports whether r names a call of tool with args (a hook's
// if = "shell(git push *)").
func (r PermissionRule) matchesCall(tool string, args map[string]any) bool {
	if r.Kind == RuleShell { // any command of the script, as the command policy sees it
		for _, script := range callTargets(tool, args)[RuleShell] {
			cmds, err := extractCommands(script, 0)
			if err != nil {
				continue
			}
			for _, c := range cmds {
				if _, ok := matchAny(r.cmd, c); ok {
					return true
				}
			}
		}
		return false
	}
	for _, t := range callTargets(tool, args)[r.Kind] {
		if r.matches(t) {
			return true
		}
	}
	return false
}

// callGrants are tool calls a pre_tool hook allowed, by the call's ID: the
// approvals the call would ask for are granted (deny and ask rules still
// apply).
type callGrants struct {
	mu  sync.Mutex
	ids map[string]bool
}

// GrantCall lets the call id through its approvals (a pre_tool hook's
// allow); EndCall forgets it.
func (h *Hooks) GrantCall(id string) {
	if id == "" {
		return
	}
	h.grants.mu.Lock()
	defer h.grants.mu.Unlock()
	if h.grants.ids == nil {
		h.grants.ids = map[string]bool{}
	}
	h.grants.ids[id] = true
}

// EndCall forgets the call's grant.
func (h *Hooks) EndCall(id string) {
	h.grants.mu.Lock()
	defer h.grants.mu.Unlock()
	delete(h.grants.ids, id)
}

// granted reports whether ctx is a tool call a hook allowed.
func (h *Hooks) granted(ctx context.Context) bool {
	id := CallID(ctx)
	if id == "" {
		return false
	}
	h.grants.mu.Lock()
	defer h.grants.mu.Unlock()
	return h.grants.ids[id]
}

// CallID is the ID of the tool call ctx runs (the tool's context), or "".
func CallID(ctx context.Context) string {
	if c, ok := ctx.(interface{ FunctionCallID() string }); ok {
		return c.FunctionCallID()
	}
	return ""
}
