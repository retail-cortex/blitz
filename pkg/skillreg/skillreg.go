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

// Package skillreg installs skills from registries (spec_skills_013
// SK-90..SK-96): a registry is a URL serving a JSON index of skills, each
// a .zip archive pinned by its SHA-256, as the Castor skills registry or
// anyone else can serve it. The registries the user added, and the skills
// installed from them, are kept in ~/.blitz/skill-registries.toml; a
// registry's token is kept in the system keychain. Installed skills are
// ordinary skill folders (~/.blitz/skills/<name>, or a project's
// .agents/skills/<name>), loaded as any other, skills.policy and all.
package skillreg

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/secrets"
)

// IndexVersion is the index format this package reads.
const IndexVersion = 1

// Index is what a registry serves.
type Index struct {
	Version int     `json:"version"`
	Name    string  `json:"name"`
	Skills  []Entry `json:"skills"`
}

// Entry is a skill a registry offers.
type Entry struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Version     string `json:"version"`
	// ArchiveURL is the skill's .zip: absolute, or relative to the index.
	ArchiveURL string `json:"archive_url"`
	// SHA256 is the archive's hash, hex; an archive that doesn't match is
	// refused.
	SHA256 string `json:"sha256"`
	// CastorID is the skill's id in the Castor registry, when it has one.
	CastorID string `json:"castor_id,omitempty"`
}

// Registry is one the user added.
type Registry struct {
	Name string `toml:"name"`
	URL  string `toml:"url"`
}

// Installed is a skill installed from a registry.
type Installed struct {
	Name     string    `toml:"name"`
	Registry string    `toml:"registry"`
	Version  string    `toml:"version"`
	SHA256   string    `toml:"sha256"`
	Dir      string    `toml:"dir"` // the skill's folder
	Time     time.Time `toml:"time"`
}

// Found is a search hit: a registry's entry.
type Found struct {
	Registry string
	Entry
}

// Errors.
var (
	ErrNoRegistry   = errors.New("no such registry")
	ErrNotOffered   = errors.New("no registry offers that skill")
	ErrAmbiguous    = errors.New("several registries offer that skill")
	ErrHash         = errors.New("the archive doesn't match the hash the registry gives")
	ErrNotInstalled = errors.New("not installed from a registry")
	ErrExists       = errors.New("a skill of that name is already there")
)

// Limits (variables, so tests can lower them).
var (
	maxIndex   int64 = 4 << 20
	maxArchive int64 = 32 << 20
	maxFiles         = 1000
	maxUnpack  int64 = 64 << 20
)

// timeout bounds one fetch.
const timeout = 30 * time.Second

var validName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// Store is the user's registries and the skills installed from them.
type Store struct {
	// File holds the registries and the install records.
	File string
	// UserSkills is where a skill for the user goes.
	UserSkills string
	// Secrets keeps registries' tokens.
	Secrets secrets.Store
	// Client fetches indexes and archives (default: one with a timeout).
	Client *http.Client
}

// Default is the user's store.
func Default() *Store {
	dir := config.ExpandHome("~/.blitz")
	return &Store{File: filepath.Join(dir, "skill-registries.toml"), UserSkills: filepath.Join(dir, "skills"), Secrets: secrets.Default(dir)}
}

type state struct {
	Registries []Registry  `toml:"registries"`
	Installed  []Installed `toml:"installed"`
}

func (s *Store) load() (state, error) {
	var st state
	if _, err := toml.DecodeFile(s.File, &st); err != nil && !errors.Is(err, os.ErrNotExist) {
		return st, fmt.Errorf("%s: %w", filepath.Base(s.File), err)
	}
	return st, nil
}

func (s *Store) save(st state) error {
	if err := os.MkdirAll(filepath.Dir(s.File), 0o700); err != nil {
		return err
	}
	var buf bytes.Buffer
	buf.WriteString("# Skill registries and the skills installed from them (blitz skills).\n")
	if err := toml.NewEncoder(&buf).Encode(st); err != nil {
		return err
	}
	tmp := s.File + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.File)
}

func tokenKey(registry string) string { return "skill-registry:" + registry }

// Registries are those the user added, by name.
func (s *Store) Registries() ([]Registry, error) {
	st, err := s.load()
	return st.Registries, err
}

// Add checks the index at rawURL and remembers the registry as name (the
// index's own name when empty), with token for it (kept in the keychain;
// "" for none). It replaces a registry of the same name.
func (s *Store) Add(ctx context.Context, name, rawURL, token string) (*Index, error) {
	if err := checkURL(rawURL); err != nil {
		return nil, err
	}
	idx, err := s.fetchIndex(ctx, Registry{URL: rawURL}, token)
	if err != nil {
		return nil, err
	}
	if name == "" {
		name = strings.ToLower(idx.Name)
	}
	if !validName.MatchString(name) {
		return nil, fmt.Errorf("registry name %q: use lowercase letters, digits, '.', '-' and '_'", name)
	}
	st, err := s.load()
	if err != nil {
		return nil, err
	}
	if token != "" {
		if err := s.Secrets.Set(tokenKey(name), token); err != nil {
			return nil, fmt.Errorf("keeping the token: %w", err)
		}
	} else {
		_ = s.Secrets.Delete(tokenKey(name))
	}
	st.Registries = append(slices.DeleteFunc(st.Registries, func(r Registry) bool { return r.Name == name }), Registry{Name: name, URL: rawURL})
	return idx, s.save(st)
}

// Remove forgets a registry and its token; skills installed from it stay.
func (s *Store) Remove(name string) error {
	st, err := s.load()
	if err != nil {
		return err
	}
	n := len(st.Registries)
	st.Registries = slices.DeleteFunc(st.Registries, func(r Registry) bool { return r.Name == name })
	if len(st.Registries) == n {
		return fmt.Errorf("%w: %s", ErrNoRegistry, name)
	}
	_ = s.Secrets.Delete(tokenKey(name))
	return s.save(st)
}

// checkURL allows https, http to this machine, and local files.
func checkURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || len(u.Scheme) == 1 { // a Windows drive letter is a path
		return nil
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if h := u.Hostname(); h == "localhost" || h == "127.0.0.1" || h == "::1" {
			return nil
		}
		return fmt.Errorf("%s: a registry needs https (http only for localhost)", raw)
	}
	return fmt.Errorf("%s: a registry is an https URL or a local file", raw)
}

func isRemote(raw string) bool {
	return strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "http://")
}

func (s *Store) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{Timeout: timeout}
}

// get reads a URL or a local file, at most limit bytes; the token goes
// only to the registry's own host.
func (s *Store) get(ctx context.Context, raw, registryURL, token string, limit int64) ([]byte, error) {
	if !isRemote(raw) {
		f, err := os.Open(raw)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return readLimited(f, raw, limit)
	}
	if err := checkURL(raw); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	if token != "" && sameHost(raw, registryURL) {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := s.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", redact(raw), resp.StatusCode)
	}
	return readLimited(resp.Body, redact(raw), limit)
}

func readLimited(r io.Reader, what string, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err == nil && int64(len(data)) > limit {
		err = fmt.Errorf("%s is larger than %d MB", what, limit>>20)
	}
	return data, err
}

func sameHost(a, b string) bool {
	ua, err1 := url.Parse(a)
	ub, err2 := url.Parse(b)
	return err1 == nil && err2 == nil && ua.Scheme == ub.Scheme && ua.Host == ub.Host
}

// redact drops a URL's user info and query, which may hold credentials.
func redact(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.User, u.RawQuery = nil, ""
	return u.String()
}

// HasToken reports whether the registry has a token in the keychain.
func (s *Store) HasToken(name string) bool { return s.token(name) != "" }

func (s *Store) token(name string) string {
	if name == "" || s.Secrets == nil {
		return ""
	}
	t, _ := s.Secrets.Get(tokenKey(name))
	return t
}

func (s *Store) fetchIndex(ctx context.Context, r Registry, token string) (*Index, error) {
	data, err := s.get(ctx, r.URL, r.URL, token, maxIndex)
	if err != nil {
		return nil, err
	}
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("%s: not a skills index: %w", redact(r.URL), err)
	}
	if idx.Version != IndexVersion {
		return nil, fmt.Errorf("%s: index version %d (this Blitz reads %d)", redact(r.URL), idx.Version, IndexVersion)
	}
	return &idx, nil
}

func (s *Store) registry(name string) (Registry, error) {
	list, err := s.Registries()
	if err != nil {
		return Registry{}, err
	}
	for _, r := range list {
		if r.Name == name {
			return r, nil
		}
	}
	return Registry{}, fmt.Errorf("%w: %s", ErrNoRegistry, name)
}

// Search returns the skills whose name or description has every word of
// query (all of them for ""), from every registry; a registry that can't
// be read is reported in err while the others' hits still come back.
func (s *Store) Search(ctx context.Context, query string) ([]Found, error) {
	list, err := s.Registries()
	if err != nil {
		return nil, err
	}
	words := strings.Fields(strings.ToLower(query))
	var out []Found
	var errs []error
	for _, r := range list {
		idx, err := s.fetchIndex(ctx, r, s.token(r.Name))
		if err != nil {
			errs = append(errs, fmt.Errorf("registry %s: %w", r.Name, err))
			continue
		}
		for _, e := range idx.Skills {
			text := strings.ToLower(e.Name + " " + e.Description)
			if !slices.ContainsFunc(words, func(w string) bool { return !strings.Contains(text, w) }) {
				out = append(out, Found{Registry: r.Name, Entry: e})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Registry < out[j].Registry
	})
	return out, errors.Join(errs...)
}

// Installed lists the skills installed from registries.
func (s *Store) Installed() ([]Installed, error) {
	st, err := s.load()
	return st.Installed, err
}

// find picks the entry ref names: "registry/name", or a name only one
// registry offers.
func (s *Store) find(ctx context.Context, ref string) (Registry, Entry, error) {
	regName, name, qualified := strings.Cut(ref, "/")
	if !qualified {
		regName, name = "", ref
	}
	list, err := s.Registries()
	if err != nil {
		return Registry{}, Entry{}, err
	}
	var hits []Found
	var regs []Registry
	var errs []error
	for _, r := range list {
		if regName != "" && r.Name != regName {
			continue
		}
		idx, err := s.fetchIndex(ctx, r, s.token(r.Name))
		if err != nil {
			errs = append(errs, fmt.Errorf("registry %s: %w", r.Name, err))
			continue
		}
		for _, e := range idx.Skills {
			if e.Name == name {
				hits, regs = append(hits, Found{Registry: r.Name, Entry: e}), append(regs, r)
			}
		}
	}
	switch {
	case regName != "" && len(regs) == 0 && len(errs) == 0 && !slices.ContainsFunc(list, func(r Registry) bool { return r.Name == regName }):
		return Registry{}, Entry{}, fmt.Errorf("%w: %s", ErrNoRegistry, regName)
	case len(hits) == 0:
		return Registry{}, Entry{}, errors.Join(fmt.Errorf("%w: %s", ErrNotOffered, ref), errors.Join(errs...))
	case len(hits) > 1:
		return Registry{}, Entry{}, fmt.Errorf("%w: %s; say which, as <registry>/%s", ErrAmbiguous, name, name)
	}
	return regs[0], hits[0].Entry, nil
}

// Install fetches the skill ref names ("registry/name", or a name one
// registry offers), checks its archive's hash, and installs it for the
// user, or in the project at projectDir (.agents/skills) when that isn't
// "". It replaces a skill it installed before, never one it didn't.
func (s *Store) Install(ctx context.Context, ref, projectDir string) (Installed, error) {
	r, e, err := s.find(ctx, ref)
	if err != nil {
		return Installed{}, err
	}
	return s.install(ctx, r, e, projectDir)
}

func (s *Store) install(ctx context.Context, r Registry, e Entry, projectDir string) (Installed, error) {
	if !validName.MatchString(e.Name) {
		return Installed{}, fmt.Errorf("registry %s offers a skill named %q, which isn't a valid name", r.Name, e.Name)
	}
	want := strings.ToLower(e.SHA256)
	if len(want) != 64 {
		return Installed{}, fmt.Errorf("registry %s gives no SHA-256 for %s: it can't be checked", r.Name, e.Name)
	}
	src, err := resolve(r.URL, e.ArchiveURL)
	if err != nil {
		return Installed{}, err
	}
	data, err := s.get(ctx, src, r.URL, s.token(r.Name), maxArchive)
	if err != nil {
		return Installed{}, err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != want {
		return Installed{}, fmt.Errorf("%w: %s from %s", ErrHash, e.Name, r.Name)
	}
	root := s.UserSkills
	if projectDir != "" {
		root = filepath.Join(projectDir, ".agents", "skills")
	}
	dst := filepath.Join(root, e.Name)
	st, err := s.load()
	if err != nil {
		return Installed{}, err
	}
	if _, err := os.Stat(dst); err == nil && !slices.ContainsFunc(st.Installed, func(i Installed) bool { return i.Dir == dst }) {
		return Installed{}, fmt.Errorf("%w: %s", ErrExists, dst)
	}
	staging := filepath.Join(root, "."+e.Name+".new")
	os.RemoveAll(staging)
	if err := unpack(data, staging, e.Name); err != nil {
		os.RemoveAll(staging)
		return Installed{}, fmt.Errorf("%s: %w", e.Name, err)
	}
	old := filepath.Join(root, "."+e.Name+".old")
	os.RemoveAll(old)
	if err := os.Rename(dst, old); err != nil && !errors.Is(err, os.ErrNotExist) {
		os.RemoveAll(staging)
		return Installed{}, err
	}
	if err := os.Rename(staging, dst); err != nil {
		_ = os.Rename(old, dst)
		os.RemoveAll(staging)
		return Installed{}, err
	}
	os.RemoveAll(old)
	i := Installed{Name: e.Name, Registry: r.Name, Version: e.Version, SHA256: want, Dir: dst, Time: time.Now().UTC().Truncate(time.Second)}
	st.Installed = append(slices.DeleteFunc(st.Installed, func(x Installed) bool { return x.Dir == dst }), i)
	return i, s.save(st)
}

// resolve is an archive's location: absolute, or relative to the index.
func resolve(index, archive string) (string, error) {
	if isRemote(archive) {
		return archive, nil
	}
	if isRemote(index) {
		base, err := url.Parse(index)
		if err != nil {
			return "", err
		}
		ref, err := url.Parse(archive)
		if err != nil {
			return "", err
		}
		return base.ResolveReference(ref).String(), nil
	}
	if filepath.IsAbs(archive) {
		return archive, nil
	}
	return filepath.Join(filepath.Dir(index), filepath.FromSlash(archive)), nil
}

// nameLine is a SKILL.md's name in its front matter.
var nameLine = regexp.MustCompile(`(?m)^name:\s*["']?([^"'\r\n]+?)["']?\s*$`)

// unpack extracts a skill's .zip into dir: SKILL.md at its root or in its
// one folder, naming the skill name; only regular files, inside the
// archive, within the limits.
func unpack(data []byte, dir, name string) error {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("not a .zip: %w", err)
	}
	if len(z.File) > maxFiles {
		return fmt.Errorf("more than %d files", maxFiles)
	}
	// The skill's root: where SKILL.md is, at the top or one folder down.
	prefix := ""
	for _, f := range z.File {
		if f.Name == "SKILL.md" {
			prefix = ""
			break
		}
		if d, rest, ok := strings.Cut(f.Name, "/"); ok && rest == "SKILL.md" {
			prefix = d + "/"
		}
	}
	var total uint64
	found := false
	for _, f := range z.File {
		rel, ok := strings.CutPrefix(f.Name, prefix)
		if !ok || rel == "" {
			continue
		}
		local := filepath.FromSlash(rel)
		if !filepath.IsLocal(local) {
			return fmt.Errorf("%q leaves the archive", f.Name)
		}
		if f.FileInfo().IsDir() {
			continue
		}
		if !f.Mode().IsRegular() {
			continue // no links
		}
		if total += f.UncompressedSize64; total > uint64(maxUnpack) {
			return fmt.Errorf("larger than %d MB unpacked", maxUnpack>>20)
		}
		dst := filepath.Join(dir, local)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := extract(f, dst); err != nil {
			return err
		}
		if rel == "SKILL.md" {
			found = true
			md, err := os.ReadFile(dst)
			if err != nil {
				return err
			}
			m := nameLine.FindSubmatch(md)
			if m == nil || string(m[1]) != name {
				return fmt.Errorf("its SKILL.md doesn't name the skill %q", name)
			}
		}
	}
	if !found {
		return errors.New("no SKILL.md in the archive")
	}
	return nil
}

func extract(f *zip.File, dst string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, (f.Mode().Perm()&0o755)|0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, io.LimitReader(rc, maxUnpack))
	return errors.Join(err, out.Close())
}

// Update reinstalls the skills installed from registries (those named, or
// all) whose registry now offers another version or archive. It returns
// those it updated.
func (s *Store) Update(ctx context.Context, names ...string) ([]Installed, error) {
	list, err := s.Installed()
	if err != nil {
		return nil, err
	}
	var done []Installed
	var errs []error
	for _, i := range list {
		if len(names) > 0 && !slices.Contains(names, i.Name) {
			continue
		}
		r, err := s.registry(i.Registry)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", i.Name, err))
			continue
		}
		idx, err := s.fetchIndex(ctx, r, s.token(r.Name))
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", i.Name, err))
			continue
		}
		at := slices.IndexFunc(idx.Skills, func(e Entry) bool { return e.Name == i.Name })
		if at < 0 {
			errs = append(errs, fmt.Errorf("%s: registry %s no longer offers it", i.Name, r.Name))
			continue
		}
		e := idx.Skills[at]
		if strings.EqualFold(e.SHA256, i.SHA256) {
			continue // up to date
		}
		project := ""
		if filepath.Dir(i.Dir) != s.UserSkills {
			project = filepath.Dir(filepath.Dir(filepath.Dir(i.Dir))) // <project>/.agents/skills/<name>
		}
		n, err := s.install(ctx, r, e, project)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", i.Name, err))
			continue
		}
		done = append(done, n)
	}
	for _, n := range names {
		if !slices.ContainsFunc(list, func(i Installed) bool { return i.Name == n }) {
			errs = append(errs, fmt.Errorf("%w: %s", ErrNotInstalled, n))
		}
	}
	return done, errors.Join(errs...)
}

// Uninstall removes a skill installed from a registry (the user's, or the
// project's at projectDir when the user has none of that name), and its
// record; a skill it didn't install is left alone.
func (s *Store) Uninstall(name, projectDir string) (Installed, error) {
	st, err := s.load()
	if err != nil {
		return Installed{}, err
	}
	at := -1
	for _, dir := range []string{filepath.Join(s.UserSkills, name), filepath.Join(projectDir, ".agents", "skills", name)} {
		if at = slices.IndexFunc(st.Installed, func(i Installed) bool { return i.Dir == dir }); at >= 0 {
			break
		}
	}
	if at < 0 {
		return Installed{}, fmt.Errorf("%w: %s", ErrNotInstalled, name)
	}
	i := st.Installed[at]
	if err := os.RemoveAll(i.Dir); err != nil {
		return Installed{}, err
	}
	st.Installed = slices.Delete(st.Installed, at, at+1)
	return i, s.save(st)
}
