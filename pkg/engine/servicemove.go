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

package engine

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/workers"
	"github.com/retail-cortex/blitz/pkg/jobspec"
)

// local finds an address on this computer, as a job or its tools write it.
var local = regexp.MustCompile(`(?i)\b(?:localhost|127\.0\.0\.1|0\.0\.0\.0|\[::1\])(?::\d+)?\b`)

// absPath finds an absolute path (or one from the home folder) in text.
var absPath = regexp.MustCompile(`(?:^|[\s"'(=,:])((?:~/|/)[\w.@-]+(?:/[\w.@-]+)*/?)`)

// outside is whether p, a path as written, is outside the workspace dir.
func outside(p, dir string) bool {
	if strings.HasPrefix(p, "~") {
		return true
	}
	rel, err := filepath.Rel(dir, filepath.Clean(p))
	return err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// ServiceProblems says what keeps the job name from running on the
// organization's service as it runs here (spec_053_enterprise EN-41): an
// MCP server that's a program on this computer or at a local address, a
// connector reaching this computer, a file outside the workspace, or a
// local address in the job itself. None means it can move.
func (w *Workspace) ServiceProblems(name string) ([]string, error) {
	wk, _, err := w.worker(name)
	if err != nil {
		return nil, err
	}
	var out []string
	add := func(format string, a ...any) {
		if p := fmt.Sprintf(format, a...); !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	for _, s := range w.liveMCP() {
		switch {
		case s.Disabled || s.InProcess != "":
		case s.Command != "":
			add("the MCP server %s is a program on this computer (%s): the service can't start it", s.Name, s.Command)
		case local.MatchString(s.URL):
			add("the MCP server %s is at %s, on this computer", s.Name, s.URL)
		}
	}
	for _, c := range w.Connectors() {
		if !c.On {
			continue
		}
		for _, k := range sortedKeys(c.Settings) {
			v := c.Settings[k]
			switch {
			case local.MatchString(v):
				add("the connector %s reaches %s, on this computer", c.Name, local.FindString(v))
			case (filepath.IsAbs(v) || strings.HasPrefix(v, "~/")) && outside(v, w.Dir()):
				add("the connector %s reads %s, outside the workspace", c.Name, v)
			}
		}
	}
	// The job as written: a permission the parser refused still names
	// what the job reaches.
	text := wk.Prompt
	if spec, _, err := workers.ReadSpec(wk.Dir); err == nil {
		text = spec.Prompt + "\n" + strings.Join(spec.Permissions, "\n")
	}
	for _, m := range absPath.FindAllStringSubmatch(text, -1) {
		if p := m[1]; outside(p, w.Dir()) && !strings.HasPrefix(p, "//") {
			add("it names %s, outside the workspace: the service has only the workspace", p)
		}
	}
	for _, m := range local.FindAllString(text, -1) {
		add("it reaches %s, which on the service is the runner itself", m)
	}
	return out, nil
}

// sortedKeys are m's keys in order.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// MoveToService makes the job name run on the organization's service
// (runs_on: service, spec_053 EN-40), all else as written: unless force,
// only when nothing keeps it from running there (ServiceProblems, which it
// returns either way). moved says whether it was.
func (w *Workspace) MoveToService(name string, force bool) (problems []string, info api.WorkerInfo, moved bool, err error) {
	if problems, err = w.ServiceProblems(name); err != nil || (len(problems) > 0 && !force) {
		return problems, api.WorkerInfo{}, false, err
	}
	dir, err := w.workerDir(name)
	if err != nil {
		return problems, api.WorkerInfo{}, false, err
	}
	spec, hash, err := workers.ReadSpec(dir)
	if err != nil {
		return problems, api.WorkerInfo{}, false, err
	}
	spec.RunsOn = jobspec.Service
	info, invalid, err := w.UpdateWorker(spec, hash)
	if err == nil && len(invalid) > 0 {
		err = fmt.Errorf("the job doesn't read: %s", strings.Join(invalid, "; "))
	}
	return problems, info, err == nil, err
}
