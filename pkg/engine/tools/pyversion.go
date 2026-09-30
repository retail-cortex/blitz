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
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/retail-cortex/blitz/pkg/api"
)

// A script's interpreter constraint (BL-SK-03): requires_python in its
// definition, else requires-python in its PEP 723 metadata. The system
// Python runs it when it satisfies the constraint; otherwise uv finds a
// managed interpreter that does (installing one after an approval), and
// without uv the script is refused with the reason.

// pep723RE finds a PEP 723 "script" metadata block.
var pep723RE = regexp.MustCompile(`(?m)^# /// script\s*$((?:\n#(?: .*)?$)*?)\n# ///\s*$`)

// scriptRequiresPython is requires-python from a script's PEP 723 metadata
// ("" when it has none).
func scriptRequiresPython(src string) string {
	m := pep723RE.FindStringSubmatch(src)
	if m == nil {
		return ""
	}
	var body strings.Builder
	for line := range strings.SplitSeq(strings.TrimPrefix(m[1], "\n"), "\n") {
		line = strings.TrimPrefix(line, "#")
		body.WriteString(strings.TrimPrefix(line, " ") + "\n")
	}
	var meta struct {
		RequiresPython string `toml:"requires-python"`
	}
	if _, err := toml.Decode(body.String(), &meta); err != nil {
		return ""
	}
	return strings.TrimSpace(meta.RequiresPython)
}

var pythonVersionRE = regexp.MustCompile(`(\d+)\.(\d+)(?:\.(\d+))?`)

// parseVersion reads "3.11", "3.11.4" or "Python 3.11.4" as release numbers.
func parseVersion(s string) ([]int, bool) {
	m := pythonVersionRE.FindStringSubmatch(s)
	if m == nil {
		return nil, false
	}
	var v []int
	for _, part := range m[1:] {
		if part == "" {
			continue
		}
		n, _ := strconv.Atoi(part)
		v = append(v, n)
	}
	return v, true
}

// parseRelease reads a release number such as 3, 3.11 or 3.11.4.
func parseRelease(s string) []int {
	var v []int
	for part := range strings.SplitSeq(strings.Trim(s, "."), ".") {
		n, _ := strconv.Atoi(part)
		v = append(v, n)
	}
	return v
}

// versionString writes release numbers as 3.11.4.
func versionString(v []int) string {
	parts := make([]string, len(v))
	for i, n := range v {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ".")
}

// compareVersions compares release numbers, the shorter padded with zeros.
func compareVersions(a, b []int) int {
	for i := range max(len(a), len(b)) {
		x, y := 0, 0
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// prefixMatch reports whether v starts with p (for ==3.11.*).
func prefixMatch(v, p []int) bool {
	for i, n := range p {
		if i >= len(v) || v[i] != n {
			return false
		}
	}
	return true
}

var clauseRE = regexp.MustCompile(`^\s*(~=|===|==|!=|<=|>=|<|>)\s*([0-9][0-9.]*)(\.\*)?\s*$`)

// satisfies reports whether version v meets a PEP 440 specifier set of
// release versions (">=3.10,<3.13", "~=3.11", "==3.12.*").
func satisfies(v []int, spec string) (bool, error) {
	for clause := range strings.SplitSeq(spec, ",") {
		if strings.TrimSpace(clause) == "" {
			continue
		}
		m := clauseRE.FindStringSubmatch(clause)
		if m == nil {
			return false, fmt.Errorf("can't read the Python requirement %q", spec)
		}
		want := parseRelease(m[2])
		wild := m[3] != ""
		c := compareVersions(v, want)
		var ok bool
		switch m[1] {
		case "==", "===":
			ok = (wild && prefixMatch(v, want)) || (!wild && c == 0)
		case "!=":
			ok = !((wild && prefixMatch(v, want)) || (!wild && c == 0))
		case ">=":
			ok = c >= 0
		case "<=":
			ok = c <= 0
		case ">":
			ok = c > 0
		case "<":
			ok = c < 0
		case "~=": // compatible release: >= want, and the same up to its last part
			ok = c >= 0 && len(want) > 1 && prefixMatch(v, want[:len(want)-1])
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

// pythonVersion asks an interpreter its version.
func pythonVersion(python string) ([]int, error) {
	out, err := exec.Command(python, "--version").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s --version: %w", python, err)
	}
	v, ok := parseVersion(string(out))
	if !ok {
		return nil, fmt.Errorf("%s --version said %q", python, strings.TrimSpace(string(out)))
	}
	return v, nil
}

// uvFind is the uv-managed (or other) interpreter meeting spec, without
// downloading one ("" when there is none).
func uvFind(ctx context.Context, uv, spec string) string {
	cmd := exec.CommandContext(ctx, uv, "python", "find", "--no-config", spec)
	cmd.Env = append(cmd.Environ(), "UV_PYTHON_DOWNLOADS=never", "UV_NO_CONFIG=1")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	p := strings.TrimSpace(string(out))
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real
	}
	return p
}

// pythonFor is the interpreter for a script needing spec: the system's
// when it meets it, else a uv-managed one (installed, after an approval,
// inside box, when none is yet).
func (s *SkillScripts) pythonFor(ctx context.Context, box ScriptBox, skill, spec string) (string, error) {
	system, err := SystemPython()
	if err != nil {
		return "", err
	}
	if spec == "" {
		return system, nil
	}
	v, err := pythonVersion(system)
	if err != nil {
		return "", err
	}
	ok, err := satisfies(v, spec)
	if err != nil {
		return "", err
	}
	if ok {
		return system, nil
	}
	have := versionString(v)
	uv, err := findUV()
	if err != nil {
		return "", fmt.Errorf("the script needs Python %s and the system's is %s: install uv (https://docs.astral.sh/uv/) to use a managed interpreter", spec, have)
	}
	if p := uvFind(ctx, uv, spec); p != "" {
		return p, nil
	}
	if err := s.hooks.Approve(ctx, api.ApprovalRequest{
		Tool: "run_skill_script", Kind: api.ActionNetwork,
		Detail: fmt.Sprintf("Install a uv-managed Python %s for skill %s (the system's is %s): uv python install '%s'", spec, skill, have, spec),
		Key:    "uvpython:" + spec, KeyLabel: "installing Python " + spec,
	}); err != nil {
		return "", err
	}
	if err := uvInstall(ctx, box, uv, spec); err != nil {
		return "", err
	}
	if p := uvFind(ctx, uv, spec); p != "" {
		return p, nil
	}
	return "", fmt.Errorf("uv installed Python %s but can't find it", spec)
}

// uvInstall installs a managed Python meeting spec, inside box: network on,
// writes only to uv's Python directory and cache.
func uvInstall(ctx context.Context, box ScriptBox, uv, spec string) error {
	dirOut, err := exec.CommandContext(ctx, uv, "python", "dir").Output()
	if err != nil {
		return fmt.Errorf("uv python dir: %w", err)
	}
	pyDir := strings.TrimSpace(string(dirOut))
	cacheOut, err := exec.CommandContext(ctx, uv, "cache", "dir").Output()
	if err != nil {
		return fmt.Errorf("uv cache dir: %w", err)
	}
	cache := strings.TrimSpace(string(cacheOut))
	ctx, cancel := context.WithTimeout(ctx, pyEnvBuildLimit)
	defer cancel()
	var out bytes.Buffer
	res, err := box.Run(ctx, ScriptRequest{
		Argv: []string{uv, "python", "install", "--no-config", spec}, Dir: pyDir,
		Env:     []string{"UV_PYTHON_INSTALL_DIR=" + pyDir, "UV_CACHE_DIR=" + cache, "UV_NO_CONFIG=1"},
		Network: true, ReadOnly: []string{filepath.Dir(uv)}, Writable: []string{pyDir, cache},
		Stdout: &out, Stderr: &out,
	})
	switch {
	case err != nil:
		return fmt.Errorf("uv python install: %w", err)
	case res.TimedOut:
		return fmt.Errorf("installing Python took longer than %s", pyEnvBuildLimit)
	case res.ExitCode != 0:
		return fmt.Errorf("uv python install failed (exit %d): %s", res.ExitCode, lastLines(out.String(), 12))
	}
	return nil
}
