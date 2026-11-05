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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSchedulePermissions(t *testing.T) {
	cases := []struct {
		name  string
		nodes []Node
		want  []string
	}{
		{"a save step's file", []Node{{Type: SaveFile, Path: "drafts/draft.md"}}, []string{"write:drafts/draft.md"}},
		{"a templated path's folder", []Node{{Type: SaveFile, Path: "reports/{{date}}/{{name}}.md"}}, []string{"write:reports/"}},
		{"a templated name in the folder", []Node{{Type: SaveFile, Path: "reports/day-{{n}}.md"}}, []string{"write:reports/"}},
		{"a templated root path: nothing", []Node{{Type: SaveFile, Path: "/{{x}}.md"}}, nil},
		{"a templated home path: nothing", []Node{{Type: SaveFile, Path: "~/{{x}}.md"}}, nil},
		{"a templated name in the workspace: nothing", []Node{{Type: SaveFile, Path: "{{x}}.md"}}, nil},
		{"a templated name under ./: nothing", []Node{{Type: SaveFile, Path: "./{{x}}.md"}}, nil},
		{"a templated path above the workspace: nothing", []Node{{Type: SaveFile, Path: "../{{x}}.md"}}, nil},
		{"an absolute file: nothing", []Node{{Type: SaveFile, Path: "/etc/hosts"}}, nil},
		{"a home file: nothing", []Node{{Type: SaveFile, Path: "~/notes.md"}}, nil},
		{"a file that climbs out: nothing", []Node{{Type: SaveFile, Path: "a/../../x.md"}}, nil},
		{"./ taken off", []Node{{Type: SaveFile, Path: "./out/x.md"}}, []string{"write:out/x.md"}},
		{"a call's host", []Node{{Type: APICall, URL: "https://api.example.com/v1?q=1"}, {Type: A2ACall, URL: "https://agent.example.org"}}, []string{"web:api.example.com", "web:agent.example.org"}},
		{"a templated host: nothing", []Node{{Type: APICall, URL: "https://{{host}}/x"}}, nil},
		{"an MCP tool", []Node{{Type: MCPCall, Server: "github", Tool: " create_issue "}, {Type: MCPCall, Server: "github"}}, []string{"mcp:github:create_issue"}},
		{"a browser step, once", []Node{{Type: Browser}, {Type: Browser}}, []string{"web:*"}},
		{"an agent step: nothing", []Node{{Type: Agent}}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, SchedulePermissions(Flow{Nodes: tc.nodes}))
		})
	}
}
