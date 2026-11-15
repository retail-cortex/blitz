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
// Package packages works with a workspace's assets as packages (spec_065
// PK-10 to PK-16): adds them from elsewhere (an organization's registry,
// GitHub, a folder) with a lockfile, verifies them against it, checks
// them, and compiles them into one manifest, Castor's too, for loaders
// outside Blitz.
package packages

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/retail-cortex/blitz/pkg/agentpkg"
	"github.com/retail-cortex/blitz/pkg/catalog"
)

// Kinds are every kind of asset, in the order they're listed.
var Kinds = []string{"agent", "skill", "workflow", "rule", "guardrail", "eval"}

// Root is a workspace's assets folder.
func Root(workspace string) string { return filepath.Join(workspace, ".agents") }

// Finding is what Validate found in one asset: problems keep it from
// being one (Blitz refuses it as it reads it); warnings are worth fixing.
type Finding struct {
	Key      string   `json:"asset"`
	Problems []string `json:"problems,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// Validate checks every asset under root (PK-14, Castor's audit): each
// file as Blitz reads it (pkg/catalog), and what's worth fixing: no
// description, a relative link to a file the asset hasn't. The assets
// that can't be read are problems too.
func Validate(root string) []Finding {
	assets, skipped := agentpkg.ReadAssets(root, Kinds...)
	var out []Finding
	for key, why := range skipped {
		out = append(out, Finding{Key: key, Problems: []string{why}})
	}
	for key, files := range assets {
		kind, name, _ := strings.Cut(key, "/")
		f := Finding{Key: key}
		for _, p := range slices.Sorted(mapKeys(files)) {
			if _, probs := catalog.Check(p, []byte(files[p])); len(probs) > 0 && (kind != "skill" || p == agentpkg.MainFile(kind, name) || isCatalogFile(p)) {
				f.Problems = append(f.Problems, probs...)
			}
		}
		f.Warnings = Warnings(kind, name, files)
		if len(f.Problems)+len(f.Warnings) > 0 {
			out = append(out, f)
		}
	}
	slices.SortFunc(out, func(a, b Finding) int { return strings.Compare(a.Key, b.Key) })
	return out
}

// isCatalogFile is whether the catalog reads p (a skill's scripts and
// references are the skill's, not checked one by one).
func isCatalogFile(p string) bool {
	k, _, ok := catalog.Of(p)
	return ok && k != catalog.SkillFile
}

// linkRE is a Markdown link's target.
var linkRE = regexp.MustCompile(`\]\(([^)\s]+)\)`)

// Warnings are what's worth fixing in an asset that can still be read
// (PK-14): no description; a relative link to a file it hasn't.
func Warnings(kind, name string, files map[string]string) []string {
	var out []string
	main := agentpkg.MainFile(kind, name)
	if kind != "eval" && Description(kind, files[main]) == "" {
		out = append(out, "it has no description: people and agents choose by it")
	}
	for _, p := range slices.Sorted(mapKeys(files)) {
		if !strings.HasSuffix(p, ".md") {
			continue
		}
		for _, m := range linkRE.FindAllStringSubmatch(files[p], -1) {
			target := m[1]
			if strings.Contains(target, "://") || strings.HasPrefix(target, "#") || strings.HasPrefix(target, "mailto:") || strings.HasPrefix(target, "/") {
				continue
			}
			target, _, _ = strings.Cut(target, "#")
			ref := path.Clean(path.Join(path.Dir(p), target))
			if _, ok := files[ref]; !ok {
				out = append(out, fmt.Sprintf("%s links to %s, which it hasn't", p, target))
			}
		}
	}
	return out
}

// Description is an asset's description: its front matter's (a
// workflow's top-level).
func Description(kind, text string) string {
	var meta struct {
		Description string `yaml:"description"`
	}
	src := text
	if kind != "workflow" {
		src, _ = frontMatter(text)
	}
	_ = yaml.Unmarshal([]byte(src), &meta)
	return strings.TrimSpace(meta.Description)
}

// frontMatter is text's front matter and the rest ("" and text without).
func frontMatter(text string) (string, string) {
	t := strings.TrimPrefix(text, "\ufeff")
	if !strings.HasPrefix(t, "---\n") && !strings.HasPrefix(t, "---\r\n") {
		return "", text
	}
	rest := t[strings.Index(t, "\n")+1:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", text
	}
	body := rest[end+4:]
	body = strings.TrimPrefix(strings.TrimPrefix(body, "\r"), "\n")
	return rest[:end], body
}

// deps are what an agent's or workflow's front matter says it depends on
// (an agent's skills:).
func deps(kind, text string) []agentpkg.Dep {
	if kind != "agent" {
		return nil
	}
	head, _ := frontMatter(text)
	var meta struct {
		Skills []agentpkg.Dep `yaml:"skills"`
	}
	if yaml.Unmarshal([]byte(head), &meta) != nil {
		return nil
	}
	return meta.Skills
}

// Compile writes root's assets into one manifest (PK-13): root/
// agents_manifest.json, each asset with its files, digest, version and
// source (from the lockfile, else its file), and, with castor, Castor's
// skills_manifest.json beside it for Castor's loaders (its skills: name,
// description, instructions, path, source URI and digest). It says the
// files it wrote.
func Compile(root string, castor bool) ([]string, error) {
	assets, _ := agentpkg.ReadAssets(root, Kinds...)
	lock, err := agentpkg.ReadLock(filepath.Join(root, agentpkg.LockFile))
	if err != nil {
		return nil, err
	}
	m := agentpkg.Manifest{Schema: 1, Assets: map[string]agentpkg.ManifestAsset{}}
	skills := map[string]map[string]any{}
	for key, files := range assets {
		kind, name, _ := strings.Cut(key, "/")
		main := agentpkg.MainFile(kind, name)
		a := agentpkg.ManifestAsset{Kind: kind, Name: name, Description: Description(kind, files[main]), SHA256: agentpkg.Digest(files),
			SourceURI: "file://" + main, Deps: deps(kind, files[main]), Files: files}
		if e, ok := lock.Assets[key]; ok {
			a.SourceURI, a.Version = e.URI, e.Version
		}
		m.Assets[key] = a
		if kind == "skill" {
			_, body := frontMatter(files[main])
			skills[name] = map[string]any{"name": name, "description": a.Description, "instructions": strings.TrimSpace(body), "version": a.Version,
				"path": "skills/" + name, "source_uri": a.SourceURI, "sha256_hash": a.SHA256, "references": refs(files, "skills/"+name+"/references/"), "examples": refs(files, "skills/"+name+"/examples/")}
		}
	}
	var wrote []string
	write := func(name string, v any) error {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		p := filepath.Join(root, name)
		if err := os.WriteFile(p, append(b, '\n'), 0o644); err != nil {
			return err
		}
		wrote = append(wrote, p)
		return nil
	}
	if err := write(agentpkg.ManifestFile, m); err != nil {
		return nil, err
	}
	if castor {
		if err := write("skills_manifest.json", skills); err != nil {
			return wrote, err
		}
	}
	return wrote, nil
}

// refs are a skill's files under prefix, by their name there.
func refs(files map[string]string, prefix string) map[string]string {
	out := map[string]string{}
	for p, t := range files {
		if rest, ok := strings.CutPrefix(p, prefix); ok {
			out[rest] = t
		}
	}
	return out
}

// Result is one locked asset's verification (PK-15): verified (its files
// are as added), modified (they changed since) or missing.
type Result struct {
	Key      string `json:"asset"`
	URI      string `json:"uri"`
	Status   string `json:"status"`
	Expected string `json:"expected_sha256"`
	Actual   string `json:"actual_sha256,omitempty"`
}

// The verification statuses.
const (
	Verified = "verified"
	Modified = "modified"
	Missing  = "missing"
)

// Verify checks each asset root's lockfile holds against its files now.
func Verify(root string) ([]Result, error) {
	lock, err := agentpkg.ReadLock(filepath.Join(root, agentpkg.LockFile))
	if err != nil {
		return nil, err
	}
	var out []Result
	for _, key := range lock.Keys() {
		e := lock.Assets[key]
		r := Result{Key: key, URI: e.URI, Expected: e.SHA256, Status: Verified}
		files, why := agentpkg.ReadAsset(root, e.Kind, e.Name)
		switch {
		case why != "" && len(files) == 0:
			r.Status = Missing
		default:
			r.Actual = agentpkg.Digest(files)
			if r.Actual != e.SHA256 {
				r.Status = Modified
			}
		}
		out = append(out, r)
	}
	return out, nil
}

// Registry is an organization's registry, for blitz:// packages (the
// service's spec_016 RG-10): the version a range resolves to, and its
// files.
type Registry interface {
	Fetch(ctx context.Context, host, kind, name, rng string) (version string, files map[string]string, err error)
}

// Options are how Add adds: Kind says what a bare file is (an agent, a
// rule …) when its folder doesn't; Force replaces an asset of the name
// that didn't come from a package; Registry fetches blitz:// ones;
// GitHub is GitHub's archive address (codeload.github.com) and its token.
type Options struct {
	Kind        string
	Force       bool
	Registry    Registry
	GitHubBase  string
	GitHubToken string
	HTTP        *http.Client
}

// ErrNotFetched is a package Blitz doesn't fetch itself (castor://,
// mod://, maven://): Castor's cstr does.
var ErrNotFetched = errors.New("these aren't fetched by Blitz itself: use Castor's cstr for castor://, mod:// and maven:// packages, or a github://, file:// or blitz:// address")

// ErrTaken is an asset of the name the workspace has that didn't come from
// a package (or came from another): --force replaces it.
var ErrTaken = errors.New("the workspace has its own of that name: --force replaces it")

// Add adds the package at uri to root (PK-12): its assets written where
// Blitz reads them, each recorded in the lockfile with where it came
// from, the version and its digest. It says what it added.
func Add(ctx context.Context, root, uri string, o Options) ([]agentpkg.LockEntry, error) {
	u, err := agentpkg.ParseURI(uri)
	if err != nil {
		return nil, err
	}
	lockPath := filepath.Join(root, agentpkg.LockFile)
	lock, err := agentpkg.ReadLock(lockPath)
	if err != nil {
		return nil, err
	}
	type found struct {
		kind, name, version string
		files               map[string]string
	}
	var got []found
	switch u.Scheme {
	case agentpkg.SchemeBlitz:
		if o.Registry == nil {
			return nil, errors.New("blitz:// packages come from your organization's registry: connect to it (blitz enterprise connect) or set BLITZ_REGISTRY_TOKEN")
		}
		version, files, err := o.Registry.Fetch(ctx, u.Host, u.Kind, u.Name, u.Ref)
		if err != nil {
			return nil, err
		}
		got = append(got, found{u.Kind, u.Name, version, files})
	case agentpkg.SchemeFile:
		assets, err := fromFolder(u.Target, o.Kind)
		if err != nil {
			return nil, err
		}
		for key, files := range assets {
			kind, name, _ := strings.Cut(key, "/")
			got = append(got, found{kind, name, "", files})
		}
	case agentpkg.SchemeGitHub:
		dir, err := fetchGitHub(ctx, u, o)
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(dir)
		assets, err := fromFolder(filepath.Join(dir, filepath.FromSlash(u.Subpath)), o.Kind)
		if err != nil {
			return nil, err
		}
		for key, files := range assets {
			kind, name, _ := strings.Cut(key, "/")
			got = append(got, found{kind, name, u.Ref, files})
		}
	default:
		return nil, fmt.Errorf("%s: %w", uri, ErrNotFetched)
	}
	if len(got) == 0 {
		return nil, fmt.Errorf("%s holds no agent, skill, workflow, rule, guardrail or eval", uri)
	}
	slices.SortFunc(got, func(a, b found) int { return strings.Compare(a.kind+"/"+a.name, b.kind+"/"+b.name) })
	var added []agentpkg.LockEntry
	for _, f := range got {
		key := f.kind + "/" + f.name
		// One of the name the workspace has is replaced only when it came
		// from this very package, or with Force.
		prev, locked := lock.Assets[key]
		_, err := os.Stat(filepath.Join(root, filepath.FromSlash(agentpkg.MainFile(f.kind, f.name))))
		if err == nil && !o.Force && (!locked || prev.URI != u.String()) {
			return added, fmt.Errorf("%s: %w", key, ErrTaken)
		}
		if err := agentpkg.WriteAsset(root, f.kind, f.name, f.files); err != nil {
			return added, fmt.Errorf("%s: %w", key, err)
		}
		e := agentpkg.LockEntry{Kind: f.kind, Name: f.name, URI: u.String(), Version: f.version, SHA256: agentpkg.Digest(f.files), Added: time.Now().UTC()}
		lock.Assets[key] = e
		added = append(added, e)
	}
	return added, lock.Write(lockPath)
}

// fromFolder are the assets at p: a folder holding assets as .agents
// does (agents/, skills/ …); a skill's folder (SKILL.md); a folder of
// skills; a workflow file; or another Markdown file, of kind (its
// folder's name, else kind, else an agent's).
func fromFolder(p, kind string) (map[string]map[string]string, error) {
	info, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		dir, base := filepath.Dir(p), filepath.Base(p)
		k := kind
		if name, ok := strings.CutSuffix(base, ".flow.yaml"); ok {
			k = "workflow"
			return single(dir, base, k, name)
		}
		name, ok := strings.CutSuffix(base, ".md")
		if !ok {
			return nil, fmt.Errorf("%s isn't an asset: a skill's folder, a .md or a .flow.yaml", p)
		}
		if k == "" {
			for kk, folder := range agentpkg.Folders {
				if filepath.Base(dir) == folder && kk != "skill" {
					k = kk
				}
			}
		}
		return single(dir, base, cmpOr(k, "agent"), name)
	}
	if _, err := os.Stat(filepath.Join(p, "SKILL.md")); err == nil {
		tmp, err := os.MkdirTemp("", "blitz-pkg-")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(tmp)
		name := filepath.Base(p)
		if err := copyDir(p, filepath.Join(tmp, "skills", name)); err != nil {
			return nil, err
		}
		return read(tmp, Kinds)
	}
	for _, folder := range agentpkg.Folders {
		if st, err := os.Stat(filepath.Join(p, folder)); err == nil && st.IsDir() {
			return read(p, Kinds)
		}
	}
	// A folder of skills, as Castor's roots are.
	tmp, err := os.MkdirTemp("", "blitz-pkg-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	if err := os.MkdirAll(filepath.Join(tmp, "skills"), 0o755); err != nil {
		return nil, err
	}
	entries, _ := os.ReadDir(p)
	for _, e := range entries {
		if _, err := os.Stat(filepath.Join(p, e.Name(), "SKILL.md")); e.IsDir() && err == nil {
			if err := copyDir(filepath.Join(p, e.Name()), filepath.Join(tmp, "skills", e.Name())); err != nil {
				return nil, err
			}
		}
	}
	return read(tmp, []string{"skill"})
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// single is the one asset of a file.
func single(dir, base, kind, name string) (map[string]map[string]string, error) {
	if !slices.Contains(Kinds, kind) || kind == "skill" {
		return nil, fmt.Errorf("--kind: one of agent, workflow, rule, guardrail or eval for a file")
	}
	b, err := os.ReadFile(filepath.Join(dir, base))
	if err != nil {
		return nil, err
	}
	if !agentpkg.ValidName(name) {
		return nil, fmt.Errorf("%q isn't a name Blitz gives an asset", name)
	}
	return map[string]map[string]string{kind + "/" + name: {agentpkg.MainFile(kind, name): string(b)}}, nil
}

// read are the assets under root, any that can't be read an error.
func read(root string, kinds []string) (map[string]map[string]string, error) {
	assets, skipped := agentpkg.ReadAssets(root, kinds...)
	for _, key := range slices.Sorted(mapKeys(skipped)) {
		return nil, fmt.Errorf("%s: %s", key, skipped[key])
	}
	return assets, nil
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
}

// fetchGitHub downloads u's repository at its ref into a new folder (its
// archive, as codeload.github.com serves it), and says where.
func fetchGitHub(ctx context.Context, u agentpkg.URI, o Options) (string, error) {
	base := cmpOr(o.GitHubBase, "https://codeload.github.com")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(base, "/")+"/"+u.Target+"/tar.gz/"+u.Ref, nil)
	if err != nil {
		return "", err
	}
	if o.GitHubToken != "" {
		req.Header.Set("Authorization", "Bearer "+o.GitHubToken)
	}
	hc := o.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 2 * time.Minute}
	}
	res, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github://%s@%s: GitHub answered %s", u.Target, u.Ref, res.Status)
	}
	dir, err := os.MkdirTemp("", "blitz-pkg-")
	if err != nil {
		return "", err
	}
	zr, err := gzip.NewReader(io.LimitReader(res.Body, 200<<20))
	if err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			os.RemoveAll(dir)
			return "", err
		}
		// The archive's files are under one folder, repo-ref/.
		_, rel, ok := strings.Cut(h.Name, "/")
		if !ok || rel == "" || strings.Contains(rel, "..") || h.Typeflag != tar.TypeReg {
			continue
		}
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			os.RemoveAll(dir)
			return "", err
		}
		f, err := os.Create(target)
		if err != nil {
			os.RemoveAll(dir)
			return "", err
		}
		_, err = io.Copy(f, io.LimitReader(tr, agentpkg.MaxFile+1))
		f.Close()
		if err != nil {
			os.RemoveAll(dir)
			return "", err
		}
	}
	return dir, nil
}

func mapKeys[V any](m map[string]V) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}
