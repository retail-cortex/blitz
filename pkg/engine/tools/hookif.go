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

// CallTargets are what permission rules of each kind see in a call of tool
// with args: its name, and the command, paths, host, skill or agent it
// names. A hook's if matches against them, and the engine's file rules
// take the read, write and delete paths.
func CallTargets(tool string, args map[string]any) map[string][]string {
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
	case "read_file", "list_files", "glob", "grep", "view_image", "view_document", "view_media", "lsp":
		add(RuleRead, str("path"))
	case "create_file", "replace_in_file", "edit", "delete_snippet", "notebook_edit":
		add(RuleWrite, str("path"))
	case "apply_patch": // the files the patch names
		written, deleted := PatchTargets(str("patch"))
		for _, p := range written {
			add(RuleWrite, p)
		}
		for _, p := range deleted {
			add(RuleDelete, p)
		}
	case "generate_audio":
		add(RuleRead, str("text_path"))
		add(RuleWrite, str("path"))
	case "export_pdf": // path, or markdown given
		add(RuleRead, str("path"))
		add(RuleWrite, ExportPDFOutputPath(str("path"), str("output")))
	case "export_docx":
		add(RuleRead, str("path"))
		add(RuleWrite, ExportDocxOutputPath(str("path"), str("output")))
	case "delete_file":
		add(RuleDelete, str("path"))
	case "web_fetch", "browser", "open_page":
		raw := str("url")
		if tool == "open_page" && raw != "" && !strings.Contains(raw, "://") {
			raw = "https://" + raw // as open_page takes a bare host
		}
		if u, err := url.Parse(raw); err == nil {
			add(RuleWeb, strings.ToLower(u.Hostname()))
		}
	case "activate_skill", "run_skill_script", "read_skill_file":
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
		for _, script := range CallTargets(tool, args)[RuleShell] {
			cmds, err := extractCommands(script, 0)
			for _, c := range cmds {
				if _, ok := matchAny(r.cmd, c); ok {
					return true
				}
			}
			if err != nil {
				return true // what didn't parse can't be ruled out: the hook decides
			}
		}
		return false
	}
	for _, t := range CallTargets(tool, args)[r.Kind] {
		if r.matches(t) {
			return true
		}
	}
	return false
}

// callGrants are tool calls a pre_tool hook allowed, by the call's session
// and ID (providers may number calls per response, so two sessions' calls
// can share an ID): the approvals the call would ask for are granted (deny
// and ask rules still apply).
type callGrants struct {
	mu  sync.Mutex
	ids map[string]bool
}

// callGrantKey is ctx's call's key in the grants, or "" when it has no ID.
func callGrantKey(ctx context.Context) string {
	id := CallID(ctx)
	if id == "" {
		return ""
	}
	return sessionOf(ctx) + "\x00" + id
}

// GrantCall lets ctx's tool call through its approvals (a pre_tool hook's
// allow); EndCall forgets it.
func (h *Hooks) GrantCall(ctx context.Context) {
	key := callGrantKey(ctx)
	if key == "" {
		return
	}
	h.grants.mu.Lock()
	defer h.grants.mu.Unlock()
	if h.grants.ids == nil {
		h.grants.ids = map[string]bool{}
	}
	h.grants.ids[key] = true
}

// EndCall forgets ctx's call's grant.
func (h *Hooks) EndCall(ctx context.Context) {
	h.grants.mu.Lock()
	defer h.grants.mu.Unlock()
	delete(h.grants.ids, callGrantKey(ctx))
}

// granted reports whether ctx is a tool call a hook allowed.
func (h *Hooks) granted(ctx context.Context) bool {
	key := callGrantKey(ctx)
	if key == "" {
		return false
	}
	h.grants.mu.Lock()
	defer h.grants.mu.Unlock()
	return h.grants.ids[key]
}

// CallID is the ID of the tool call ctx runs (the tool's context), or "".
func CallID(ctx context.Context) string {
	if c, ok := ctx.(interface{ FunctionCallID() string }); ok {
		return c.FunctionCallID()
	}
	return ""
}
