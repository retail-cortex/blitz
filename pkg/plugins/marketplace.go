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

package plugins

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// IndexFile is a marketplace's index.
const IndexFile = "marketplace.toml"

// Marketplace is a list of plugins to install by name
// (spec_parity_027 PAR-PLG-03).
type Marketplace struct {
	Name    string  `toml:"name"`
	Plugins []Entry `toml:"plugins"`
	// base is where relative sources are, once fetched.
	base string
}

// Entry is a plugin a marketplace offers: where to get it, and the hash
// its content must have.
type Entry struct {
	Name        string `toml:"name"`
	Version     string `toml:"version"`
	Description string `toml:"description"`
	// Source is a git repository, an https .zip, or a path relative to
	// the marketplace.
	Source string `toml:"source"`
	Hash   string `toml:"hash"`
}

// Known is a marketplace the user added.
type Known struct {
	Name string `toml:"name"`
	URL  string `toml:"url"`
}

const marketsFile = "marketplaces.toml"

// Marketplaces are those the user added.
func (s *Store) Marketplaces() ([]Known, error) {
	var f struct {
		Marketplaces []Known `toml:"marketplaces"`
	}
	if _, err := toml.DecodeFile(filepath.Join(s.Dir, marketsFile), &f); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%s: %w", marketsFile, err)
	}
	return f.Marketplaces, nil
}

func (s *Store) saveMarkets(list []Known) error {
	return s.writeFile(marketsFile, func(w io.Writer) error {
		return toml.NewEncoder(w).Encode(struct {
			Marketplaces []Known `toml:"marketplaces"`
		}{list})
	})
}

// AddMarketplace fetches the index at url (a marketplace.toml over https,
// a git repository holding one, or a local file or directory), and
// remembers it under its name.
func (s *Store) AddMarketplace(ctx context.Context, url string) (*Marketplace, error) {
	m, cleanup, err := FetchMarketplace(ctx, url)
	if err != nil {
		return nil, err
	}
	cleanup()
	list, err := s.Marketplaces()
	if err != nil {
		return nil, err
	}
	list = slices.DeleteFunc(list, func(k Known) bool { return k.Name == m.Name })
	return m, s.saveMarkets(append(list, Known{Name: m.Name, URL: url}))
}

// RemoveMarketplace forgets one.
func (s *Store) RemoveMarketplace(name string) error {
	list, err := s.Marketplaces()
	if err != nil {
		return err
	}
	n := len(list)
	list = slices.DeleteFunc(list, func(k Known) bool { return k.Name == name })
	if len(list) == n {
		return fmt.Errorf("no marketplace %q", name)
	}
	return s.saveMarkets(list)
}

// FetchMarketplace reads the index at url; cleanup removes what was
// downloaded (after the plugins it lists are fetched).
func FetchMarketplace(ctx context.Context, url string) (*Marketplace, func(), error) {
	cleanup := func() {}
	var path string
	switch {
	case gitSource.MatchString(url):
		tmp, err := os.MkdirTemp("", "blitz-market-")
		if err != nil {
			return nil, cleanup, err
		}
		cleanup = func() { os.RemoveAll(tmp) }
		cmd := exec.CommandContext(ctx, "git", "clone", "--quiet", "--depth", "1", "--", url, filepath.Join(tmp, "m"))
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			cleanup()
			return nil, func() {}, fmt.Errorf("git clone %s: %v: %s", url, err, strings.TrimSpace(string(out)))
		}
		path = filepath.Join(tmp, "m", IndexFile)
	case strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "http://"):
		tmp, err := os.MkdirTemp("", "blitz-market-")
		if err != nil {
			return nil, cleanup, err
		}
		cleanup = func() { os.RemoveAll(tmp) }
		path = filepath.Join(tmp, IndexFile)
		if err := download(ctx, url, path); err != nil {
			cleanup()
			return nil, func() {}, err
		}
	default:
		path = url
		if isDir(path) {
			path = filepath.Join(path, IndexFile)
		}
	}
	var m Marketplace
	if _, err := toml.DecodeFile(path, &m); err != nil {
		cleanup()
		return nil, func() {}, fmt.Errorf("%s: %w", IndexFile, err)
	}
	if !validName.MatchString(m.Name) {
		cleanup()
		return nil, func() {}, fmt.Errorf("%s: name %q", IndexFile, m.Name)
	}
	m.base = filepath.Dir(path)
	if strings.HasPrefix(url, "http") && !gitSource.MatchString(url) {
		m.base = strings.TrimSuffix(url, "/"+IndexFile) // relative sources are URLs next to it
	}
	return &m, cleanup, nil
}

var qualified = regexp.MustCompile(`^([a-z0-9][a-z0-9._-]*)@([a-z0-9][a-z0-9._-]*)$`)

// FromMarketplace fetches the plugin name (or name@marketplace) from the
// user's marketplaces and checks its hash. ok is false when ref isn't a
// marketplace name at all (a path or URL).
func (s *Store) FromMarketplace(ctx context.Context, ref string) (st *Staged, ok bool, err error) {
	name, market := ref, ""
	if m := qualified.FindStringSubmatch(ref); m != nil {
		name, market = m[1], m[2]
	} else if !validName.MatchString(ref) || isDir(ref) || strings.HasSuffix(ref, ".zip") {
		return nil, false, nil
	}
	known, err := s.Marketplaces()
	if err != nil {
		return nil, true, err
	}
	var found []Entry
	var from []*Marketplace
	var cleanups []func()
	defer func() {
		for _, c := range cleanups {
			c()
		}
	}()
	for _, k := range known {
		if market != "" && k.Name != market {
			continue
		}
		m, cleanup, err := FetchMarketplace(ctx, k.URL)
		if err != nil {
			return nil, true, fmt.Errorf("marketplace %s: %w", k.Name, err)
		}
		cleanups = append(cleanups, cleanup)
		for _, e := range m.Plugins {
			if e.Name == name {
				found, from = append(found, e), append(from, m)
			}
		}
	}
	switch {
	case len(found) == 0 && market == "" && len(known) == 0:
		return nil, false, nil
	case len(found) == 0:
		return nil, true, fmt.Errorf("no marketplace offers %s", ref)
	case len(found) > 1:
		return nil, true, fmt.Errorf("several marketplaces offer %s: say which, %s@<marketplace>", name, name)
	}
	e, m := found[0], from[0]
	src := e.Source
	if !gitSource.MatchString(src) && !strings.HasPrefix(src, "http") && !filepath.IsAbs(src) {
		if strings.HasPrefix(m.base, "http") {
			src = m.base + "/" + src
		} else {
			src = filepath.Join(m.base, filepath.FromSlash(src))
		}
	}
	st, err = Fetch(ctx, src)
	if err != nil {
		return nil, true, err
	}
	if st.Name != e.Name {
		st.Discard()
		return nil, true, fmt.Errorf("%s's source holds the plugin %q", e.Name, st.Name)
	}
	if e.Hash == "" {
		st.Discard()
		return nil, true, fmt.Errorf("marketplace %s gives no hash for %s: it can't be checked", m.Name, e.Name)
	}
	if h, err := Hash(st.Dir); err != nil || h != e.Hash {
		st.Discard()
		return nil, true, fmt.Errorf("%s's content doesn't match the hash marketplace %s gives (%s)", e.Name, m.Name, e.Hash)
	}
	if st.tmp == "" { // inside the marketplace: copy it out before that's removed
		tmp, err := os.MkdirTemp("", "blitz-plugin-")
		if err != nil {
			return nil, true, err
		}
		if err := copyTree(st.Dir, filepath.Join(tmp, "p")); err != nil {
			os.RemoveAll(tmp)
			return nil, true, err
		}
		st.tmp, st.Dir = tmp, filepath.Join(tmp, "p")
	}
	st.Source = name + "@" + m.Name
	return st, true, nil
}
