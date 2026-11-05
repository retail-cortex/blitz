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

// Package urn is Blitz's resource names (spec_055_urns): one URN for each
// thing an organization's service keeps, made only of IDs, such as
// urn:blitz:runs:<run-id> or
// urn:blitz:artifacts:<workspace-id>:<job-id>:<run-id>/report.csv.
package urn

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Kind is what a name names (RN-10, RN-12).
type Kind string

// The kinds.
const (
	Users      Kind = "users"
	Groups     Kind = "groups"
	Workspaces Kind = "workspaces"
	Jobs       Kind = "jobs"
	Runs       Kind = "runs"
	Content    Kind = "content"
	Artifacts  Kind = "artifacts"
)

// ids is how many IDs each kind's names take.
var ids = map[Kind]int{Users: 1, Groups: 1, Workspaces: 1, Jobs: 1, Runs: 1, Content: 1, Artifacts: 3}

// prefix starts every name.
const prefix = "urn:blitz:"

// ErrName is Parse's error for a string that isn't a resource name.
var ErrName = errors.New("not a Blitz resource name")

// uuid is an ID's form: a UUID, lower case.
var uuid = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Name is a resource name: a kind, its IDs (none for a collection, RN-03),
// and for artifacts a file's path under them.
type Name struct {
	Kind Kind
	IDs  []string
	// Path is a file's, "/"-separated and not encoded ("" for none).
	Path string
}

// New is the name of one thing of kind k with these IDs.
func New(k Kind, id ...string) (Name, error) {
	n := Name{Kind: k, IDs: id}
	return n, n.check()
}

// File is the name of a file under n, an artifacts name (RN-12).
func (n Name) File(path string) (Name, error) {
	f := Name{Kind: n.Kind, IDs: n.IDs, Path: path}
	return f, f.check()
}

// IsCollection says whether n names every thing of its kind.
func (n Name) IsCollection() bool { return len(n.IDs) == 0 }

// Parent is the name n is under: a file's thing, or for a thing with
// several IDs, the same kind with its last one dropped; ok is false for a
// collection or a thing with one ID.
func (n Name) Parent() (Name, bool) {
	switch {
	case n.Path != "":
		return Name{Kind: n.Kind, IDs: n.IDs}, true
	case len(n.IDs) > 1:
		return Name{Kind: n.Kind, IDs: n.IDs[:len(n.IDs)-1]}, true
	}
	return Name{}, false
}

func (n Name) check() error {
	want, ok := ids[n.Kind]
	switch {
	case !ok:
		return fmt.Errorf("%w: no kind %q", ErrName, n.Kind)
	case len(n.IDs) > want:
		return fmt.Errorf("%w: %s take %d IDs, not %d", ErrName, n.Kind, want, len(n.IDs))
	case n.Path != "" && (n.Kind != Artifacts || len(n.IDs) != want):
		return fmt.Errorf("%w: only one run's artifacts hold files", ErrName)
	}
	for _, id := range n.IDs {
		if !uuid.MatchString(id) {
			return fmt.Errorf("%w: %q isn't an ID", ErrName, id)
		}
	}
	if n.Path != "" {
		for _, seg := range strings.Split(n.Path, "/") {
			if seg == "" || seg == "." || seg == ".." {
				return fmt.Errorf("%w: the path %q", ErrName, n.Path)
			}
		}
	}
	return nil
}

// String is n as a URN, its path encoded (RN-01, RN-02).
func (n Name) String() string {
	var b strings.Builder
	b.WriteString(prefix)
	b.WriteString(string(n.Kind))
	for _, id := range n.IDs {
		b.WriteByte(':')
		b.WriteString(id)
	}
	if n.Path != "" {
		for _, seg := range strings.Split(n.Path, "/") {
			b.WriteByte('/')
			b.WriteString(url.PathEscape(seg))
		}
	}
	return b.String()
}

// Parse reads a resource name; urn and blitz may be in any case (RN-02),
// IDs too, and the result has them in lower case.
func Parse(s string) (Name, error) {
	if len(s) < len(prefix) || !strings.EqualFold(s[:len(prefix)], prefix) {
		return Name{}, fmt.Errorf("%w: %q", ErrName, s)
	}
	rest := s[len(prefix):]
	var n Name
	if head, path, ok := strings.Cut(rest, "/"); ok {
		rest = head
		var segs []string
		for _, seg := range strings.Split(path, "/") {
			dec, err := url.PathUnescape(seg)
			if err != nil {
				return Name{}, fmt.Errorf("%w: %q: %v", ErrName, s, err)
			}
			if strings.Contains(dec, "/") {
				return Name{}, fmt.Errorf("%w: %q: a path segment holds a /", ErrName, s)
			}
			segs = append(segs, dec)
		}
		n.Path = strings.Join(segs, "/")
		if n.Path == "" {
			return Name{}, fmt.Errorf("%w: the path %q", ErrName, path)
		}
	}
	parts := strings.Split(rest, ":")
	n.Kind = Kind(parts[0])
	for _, id := range parts[1:] {
		n.IDs = append(n.IDs, strings.ToLower(id))
	}
	if err := n.check(); err != nil {
		return Name{}, fmt.Errorf("%q: %w", s, err)
	}
	return n, nil
}

// MarshalText writes n as its URN.
func (n Name) MarshalText() ([]byte, error) { return []byte(n.String()), nil }

// UnmarshalText reads a URN into n.
func (n *Name) UnmarshalText(b []byte) error {
	p, err := Parse(string(b))
	if err != nil {
		return err
	}
	*n = p
	return nil
}
