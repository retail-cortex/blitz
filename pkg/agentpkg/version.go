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

// Package agentpkg is agentic assets as packages (spec_065_packages):
// agents, skills, rules, guardrails, workflows and evals, each a
// versioned asset another may depend on by a version range, as Blitz,
// Blitz Enterprise's library and builders outside Blitz read them.
package agentpkg

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Version is an asset's version: major.minor.patch (PK-01).
type Version struct {
	Major, Minor, Patch int
}

// ParseVersion reads "1.4.2" (a leading "v" allowed).
func ParseVersion(s string) (Version, error) {
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(s), "v"), ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("version %q: major.minor.patch, such as 1.4.2", s)
	}
	var n [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 || p != strconv.Itoa(v) {
			return Version{}, fmt.Errorf("version %q: major.minor.patch, such as 1.4.2", s)
		}
		n[i] = v
	}
	return Version{n[0], n[1], n[2]}, nil
}

// String is v as major.minor.patch.
func (v Version) String() string { return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch) }

// Compare is -1, 0 or 1 as v is before, the same as, or after w.
func (v Version) Compare(w Version) int {
	return cmp.Or(cmp.Compare(v.Major, w.Major), cmp.Compare(v.Minor, w.Minor), cmp.Compare(v.Patch, w.Patch))
}

// Bump is the version after v: "major", "minor" or "patch".
func (v Version) Bump(part string) (Version, error) {
	switch part {
	case "major":
		return Version{v.Major + 1, 0, 0}, nil
	case "minor":
		return Version{v.Major, v.Minor + 1, 0}, nil
	case "patch", "":
		return Version{v.Major, v.Minor, v.Patch + 1}, nil
	}
	return v, fmt.Errorf("bump %q: major, minor or patch", part)
}

// Range is the versions a dependency accepts (PK-02):
//
//	""  "*"  "latest"   any
//	"1.4.2"             that one
//	"1"  "1.x"          any 1.y.z
//	"1.4"  "1.4.x"      any 1.4.z
//	"^1.4.2"            1.4.2 or later, before 2.0.0 (before 0.5.0 for 0.4.x)
//	"~1.4.2"            1.4.2 or later, before 1.5.0
type Range struct {
	text     string
	lo       Version
	hi       Version // exclusive; zero: none
	any, one bool
}

// ParseRange reads a range.
func ParseRange(s string) (Range, error) {
	t := strings.TrimSpace(s)
	r := Range{text: t}
	bad := fmt.Errorf("version range %q: 1.4.2, 1.x, 1.4.x, ^1.4.2, ~1.4.2 or * (any)", s)
	switch {
	case t == "" || t == "*" || t == "latest" || t == "x":
		r.any = true
		return r, nil
	case strings.HasPrefix(t, "^"), strings.HasPrefix(t, "~"):
		v, err := ParseVersion(t[1:])
		if err != nil {
			return r, bad
		}
		r.lo = v
		switch {
		case t[0] == '~':
			r.hi = Version{v.Major, v.Minor + 1, 0}
		case v.Major > 0:
			r.hi = Version{v.Major + 1, 0, 0}
		case v.Minor > 0:
			r.hi = Version{0, v.Minor + 1, 0}
		default:
			r.hi = Version{0, 0, v.Patch + 1}
		}
		return r, nil
	}
	parts := strings.Split(strings.TrimPrefix(t, "v"), ".")
	if n := len(parts); n > 0 && (parts[n-1] == "x" || parts[n-1] == "*") {
		parts = parts[:n-1]
	}
	nums := make([]int, len(parts))
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 || p != strconv.Itoa(v) {
			return r, bad
		}
		nums[i] = v
	}
	switch len(nums) {
	case 3:
		r.lo, r.one = Version{nums[0], nums[1], nums[2]}, true
	case 2:
		r.lo, r.hi = Version{nums[0], nums[1], 0}, Version{nums[0], nums[1] + 1, 0}
	case 1:
		r.lo, r.hi = Version{nums[0], 0, 0}, Version{nums[0] + 1, 0, 0}
	default:
		return r, bad
	}
	return r, nil
}

// String is the range as written ("*" for any).
func (r Range) String() string {
	if r.text == "" {
		return "*"
	}
	return r.text
}

// Match reports whether v is in the range.
func (r Range) Match(v Version) bool {
	switch {
	case r.any:
		return true
	case r.one:
		return v == r.lo
	}
	return v.Compare(r.lo) >= 0 && v.Compare(r.hi) < 0
}

// Best is the latest of versions in the range, and whether there's one.
func (r Range) Best(versions []Version) (Version, bool) {
	var best Version
	found := false
	for _, v := range versions {
		if r.Match(v) && (!found || v.Compare(best) > 0) {
			best, found = v, true
		}
	}
	return best, found
}

// Latest is the latest of versions, and whether there's one.
func Latest(versions []Version) (Version, bool) {
	if len(versions) == 0 {
		return Version{}, false
	}
	return slices.MaxFunc(versions, Version.Compare), true
}
