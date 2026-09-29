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
	"os"
	"path/filepath"
	"strings"
)

// bwrapArgs builds a bubblewrap command line mirroring the Seatbelt profile:
// the filesystem is read-only except WritableDirs, ReadOnlyDirs stay
// read-only even inside writable ones, blocked files are replaced by
// /dev/null and blocked directories by an empty read-only tmpfs, and the
// network namespace is unshared when network access is off. The returned
// slice ends with "--"; append the command to run.
func bwrapArgs(bwrap string, spec OSSandboxSpec, blockedFiles, blockedDirs []string) []string {
	args := []string{bwrap, "--die-with-parent", "--ro-bind", "/", "/", "--dev-bind", "/dev", "/dev"}
	for _, d := range existing(spec.WritableDirs) {
		args = append(args, "--bind", d, d)
	}
	for _, d := range existing(spec.ReadOnlyDirs) {
		args = append(args, "--ro-bind", d, d)
	}
	for _, f := range blockedFiles {
		args = append(args, "--ro-bind", "/dev/null", f)
	}
	for _, d := range blockedDirs {
		args = append(args, "--tmpfs", d, "--remount-ro", d)
	}
	if !spec.AllowNetwork {
		args = append(args, "--unshare-net")
	}
	return append(args, "--")
}

func existing(dirs []string) []string {
	var out []string
	for _, d := range dirs {
		if _, err := os.Stat(d); err == nil {
			out = append(out, d)
		}
	}
	return out
}

func expandHomePattern(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}
