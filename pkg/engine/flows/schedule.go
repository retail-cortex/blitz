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

package flows

import (
	"net/url"
	"path"
	"slices"
	"strings"
)

// SchedulePermissions are what a job that runs f may do without asking
// (spec_043_workflows WF-20), worked out from its steps as they are when
// the job runs, so a step added or removed later is followed: write the
// files its save steps save, reach the hosts its service and agent calls
// call, use its MCP steps' tools, and browse (web:*) for its browser
// steps. A templated path (reports/{{name}}.md) grants its folder
// (write:reports/); one whose folder would be the whole workspace, or is
// outside it (/{{x}}.md, ~/{{x}}.md, ../{{x}}.md), grants nothing, and
// its step asks (a scheduled run: is refused) as any other write does.
func SchedulePermissions(f Flow) []string {
	var out []string
	add := func(p string) {
		if !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	for _, n := range f.Nodes {
		switch n.Type {
		case SaveFile:
			p := strings.TrimSpace(n.Path)
			if i := strings.Index(p, "{{"); i >= 0 {
				p = p[:strings.LastIndex(p[:i], "/")+1] // its folder, with its "/"
			}
			if p = workspacePath(p); p != "" {
				add("write:" + p)
			}
		case APICall, A2ACall:
			u, err := url.Parse(strings.TrimSpace(n.URL))
			if host := hostOf(u, err); host != "" {
				add("web:" + host)
			}
		case MCPCall:
			if n.Server != "" && strings.TrimSpace(n.Tool) != "" {
				add("mcp:" + n.Server + ":" + strings.TrimSpace(n.Tool))
			}
		case Browser:
			add("web:*")
		}
	}
	return out
}

// workspacePath is p as a write permission's pattern when it names a file
// or a folder inside the workspace ("" when it's empty, the workspace
// itself, absolute, in a home folder or outside it). A folder keeps its
// trailing "/", which covers everything under it.
func workspacePath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	if p == "" || strings.HasPrefix(p, "/") || strings.HasPrefix(p, "~") || strings.Contains(p, "{{") {
		return ""
	}
	clean := path.Clean(p)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return ""
	}
	if strings.HasSuffix(p, "/") {
		clean += "/"
	}
	return clean
}

// hostOf is a call's host, when its URL has one that isn't templated.
func hostOf(u *url.URL, err error) string {
	if err != nil || u == nil {
		return ""
	}
	h := u.Hostname()
	if strings.ContainsAny(h, "{}%") {
		return ""
	}
	return h
}
