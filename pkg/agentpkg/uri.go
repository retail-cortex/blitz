// Copyright 2026 Retail Cortex
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package agentpkg

import (
	"fmt"
	"strings"
)

// The schemes a package's URI may have (PK-10), Castor's among them.
const (
	SchemeBlitz  = "blitz"  // an organization's registry: blitz://<host>/<kind>/<name>[@<range>]
	SchemeGitHub = "github" // a repository's folder: github://<owner>/<repo>[@<ref>][/<path>]
	SchemeFile   = "file"   // a folder or file here: file://<path>
	SchemeCastor = "castor" // a Castor registry's skill: castor://<name>
	SchemeGo     = "mod"    // a Go module's folder: mod://<module>@<version>[/<path>]
	SchemeMaven  = "maven"  // a Maven artifact's folder: maven://<group>:<artifact>:<version>[/<path>]
)

// URI is where a package comes from (PK-10), parsed: its scheme; the
// registry's host (blitz); its target (the repository, module, artifact,
// path or name); the ref or range it pins (a branch, tag or version);
// the folder in it; and, for blitz, the asset's kind and name.
type URI struct {
	Scheme  string
	Host    string
	Target  string
	Ref     string
	Subpath string
	Kind    string
	Name    string
}

// Kinds are the kinds of asset a package is (PK-11).
var Kinds = []string{"agent", "skill", "workflow", "rule", "guardrail", "eval"}

// ParseURI reads a package's URI (PK-10): blitz://, github:// (or
// https://github.com/…, with @ref, :ref or /tree/ref, "main" when none),
// file:// (or a bare path), castor:// (and cstr://, castors://, skm://),
// mod:// (or go://) and maven:// (or mvn://), as Castor reads them.
func ParseURI(s string) (URI, error) {
	clean := strings.TrimSpace(s)
	if clean == "" {
		return URI{}, fmt.Errorf("an empty package address")
	}
	if rest, ok := strings.CutPrefix(clean, "blitz://"); ok {
		host, path, _ := strings.Cut(rest, "/")
		kind, nameRef, _ := strings.Cut(path, "/")
		name, ref, _ := strings.Cut(nameRef, "@")
		u := URI{Scheme: SchemeBlitz, Host: host, Kind: kind, Name: name, Target: kind + "/" + name, Ref: ref}
		switch {
		case host == "":
			return URI{}, fmt.Errorf("%s: name the organization's service (blitz://<host>/<kind>/<name>)", s)
		case !validKind(kind):
			return URI{}, fmt.Errorf("%s: the kind is one of %s", s, strings.Join(Kinds, ", "))
		case !ValidName(name):
			return URI{}, fmt.Errorf("%s: %q isn't a name Blitz takes", s, name)
		}
		if ref != "" {
			if _, err := ParseRange(ref); err != nil {
				return URI{}, fmt.Errorf("%s: %w", s, err)
			}
		}
		return u, nil
	}
	for _, p := range []string{"castors://", "castor://", "cstrs://", "cstr://", "skms://", "skm://"} {
		if rest, ok := strings.CutPrefix(clean, p); ok {
			name := strings.TrimPrefix(rest, "skills/")
			return URI{Scheme: SchemeCastor, Target: name, Kind: "skill", Name: name}, nil
		}
	}
	if rest, ok := strings.CutPrefix(clean, "file://"); ok {
		return URI{Scheme: SchemeFile, Target: rest}, nil
	}
	for _, p := range []string{"mod://", "go://"} {
		if rest, ok := strings.CutPrefix(clean, p); ok {
			u := URI{Scheme: SchemeGo}
			if at := strings.Index(rest, "@"); at >= 0 {
				u.Target = rest[:at]
				u.Ref, u.Subpath, _ = strings.Cut(rest[at+1:], "/")
			} else if parts := strings.Split(rest, "/"); len(parts) > 3 {
				u.Target, u.Subpath = strings.Join(parts[:3], "/"), strings.Join(parts[3:], "/")
			} else {
				u.Target = rest
			}
			return u, nil
		}
	}
	for _, p := range []string{"maven://", "mvn://"} {
		if rest, ok := strings.CutPrefix(clean, p); ok {
			u := URI{Scheme: SchemeMaven}
			u.Target, u.Subpath, _ = strings.Cut(rest, "/")
			if parts := strings.Split(u.Target, ":"); len(parts) >= 3 {
				u.Ref = parts[2]
			}
			return u, nil
		}
	}
	for _, p := range []string{"github://", "https://github.com/"} {
		if rest, ok := strings.CutPrefix(clean, p); ok {
			return parseGitHub(strings.TrimSuffix(rest, "/")), nil
		}
	}
	return URI{Scheme: SchemeFile, Target: clean}, nil
}

// parseGitHub reads owner/repo with its ref and folder in each of the
// ways Castor takes: /tree/<ref>/<path>, :<ref>[/<path>], @<ref>[/<path>],
// or owner/repo/<path> on main.
func parseGitHub(t string) URI {
	u := URI{Scheme: SchemeGitHub}
	switch {
	case strings.Contains(t, "/tree/"):
		u.Target, t, _ = strings.Cut(t, "/tree/")
		u.Ref, u.Subpath, _ = strings.Cut(t, "/")
	case strings.Contains(t, ":"):
		u.Target, t, _ = strings.Cut(t, ":")
		u.Ref, u.Subpath, _ = strings.Cut(t, "/")
	case strings.Contains(t, "@"):
		u.Target, t, _ = strings.Cut(t, "@")
		u.Ref, u.Subpath, _ = strings.Cut(t, "/")
	default:
		u.Target = t
	}
	if u.Subpath == "" {
		if parts := strings.Split(u.Target, "/"); len(parts) > 2 {
			u.Target, u.Subpath = parts[0]+"/"+parts[1], strings.Join(parts[2:], "/")
		}
	}
	if u.Ref == "" {
		u.Ref = "main"
	}
	return u
}

// String is the URI as written: blitz://, github:// and file:// as Blitz
// writes them (a lockfile's), the others with their scheme.
func (u URI) String() string {
	switch u.Scheme {
	case SchemeBlitz:
		s := "blitz://" + u.Host + "/" + u.Kind + "/" + u.Name
		if u.Ref != "" {
			s += "@" + u.Ref
		}
		return s
	case SchemeGitHub:
		s := "github://" + u.Target + "@" + u.Ref
		if u.Subpath != "" {
			s += "/" + u.Subpath
		}
		return s
	case SchemeCastor:
		return "castor://" + u.Target
	case SchemeGo:
		s := "mod://" + u.Target
		if u.Ref != "" {
			s += "@" + u.Ref
		}
		if u.Subpath != "" {
			s += "/" + u.Subpath
		}
		return s
	case SchemeMaven:
		s := "maven://" + u.Target
		if u.Subpath != "" {
			s += "/" + u.Subpath
		}
		return s
	}
	return "file://" + u.Target
}

// ValidName reports whether name is one Blitz gives an asset: lowercase
// letters, digits and single hyphens or underscores, up to 64.
func ValidName(name string) bool { return len(name) <= 64 && depName.MatchString(name) }

func validKind(k string) bool {
	for _, x := range Kinds {
		if x == k {
			return true
		}
	}
	return false
}
