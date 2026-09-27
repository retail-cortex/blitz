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

package commands

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseAndExpand(t *testing.T) {
	c, err := Parse("fix", "project", "", []byte("---\ndescription: Fix an issue\nargument-hint: <issue>\nagent: qa\nallowed-tools: Read, Grep Bash(git *)\nmode: plan\n---\nFix issue #$1 ($ARGUMENTS).\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Description != "Fix an issue" || c.ArgumentHint != "<issue>" || c.Agent != "qa" || !c.Plan ||
		!reflect.DeepEqual(c.AllowedTools, []string{"Read", "Grep", "Bash(git *)"}) {
		t.Errorf("parsed %+v", c)
	}
	if got := c.Expand("42 urgent"); got != "Fix issue #42 (42 urgent)." {
		t.Errorf("expand %q", got)
	}
	plain, _ := Parse("note", "user", "", []byte("# Summarize\nSummarize the diff."))
	if plain.Description != "Summarize" || plain.Expand("focus on tests") != "# Summarize\nSummarize the diff.\n\nfocus on tests" || plain.Expand("") != "# Summarize\nSummarize the diff." {
		t.Errorf("plain %+v / %q", plain, plain.Expand("focus on tests"))
	}
	two, _ := Parse("x", "user", "", []byte("$1 then $2, not $10"))
	if got := two.Expand("a"); got != "a then , not a0" {
		t.Errorf("positional %q", got)
	}
	for _, bad := range []string{"---\nmode: fast\n---\nx", "---\ndescription: x\n", "---\n---\n   "} {
		if _, err := Parse("b", "user", "", []byte(bad)); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}

func TestLoadNamespacesAndBundled(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "db"), 0o755)
	os.WriteFile(filepath.Join(dir, "db", "migrate.md"), []byte("Run migrations."), 0o644)
	os.WriteFile(filepath.Join(dir, "Deploy.md"), []byte("Deploy it."), 0o644)
	os.WriteFile(filepath.Join(dir, "bad name.md"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644)
	list, err := Load(dir, "project")
	var names []string
	for _, c := range list {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "db:migrate,deploy" || err == nil || !strings.Contains(err.Error(), "bad name") {
		t.Errorf("loaded %v, err %v", names, err)
	}
	if list, err := Load(filepath.Join(dir, "missing"), "user"); len(list) != 0 || err != nil {
		t.Errorf("missing dir: %v %v", list, err)
	}
	bundled := map[string]Command{}
	for _, c := range Bundled() {
		bundled[c.Name] = c
	}
	for _, name := range []string{"review", "security-review", "simplify", "verify"} {
		if _, ok := bundled[name]; !ok {
			t.Errorf("bundled command %s missing", name)
		}
	}
	if !bundled["review"].Plan || bundled["simplify"].Plan {
		t.Error("review should be read-only (plan), simplify shouldn't")
	}
}
