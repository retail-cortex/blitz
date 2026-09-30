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
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/retail-cortex/blitz/pkg/config"
)

// Installed is a plugin in the store.
type Installed struct {
	Name    string    `toml:"name"`
	Version string    `toml:"version"`
	Source  string    `toml:"source"` // where it came from, for update
	Hash    string    `toml:"hash"`
	Enabled bool      `toml:"enabled"`
	Time    time.Time `toml:"installed"`
}

// Store is where plugins are installed.
type Store struct {
	Dir string
}

// indexFile lists the installed plugins.
const indexFile = "installed.toml"

// Default is ~/.blitz/plugins.
func Default() *Store { return &Store{Dir: config.ExpandHome("~/.blitz/plugins")} }

// PluginDir is where an installed plugin's files are.
func (s *Store) PluginDir(i Installed) string { return filepath.Join(s.Dir, i.Name, i.Version) }

// ErrNotInstalled: no plugin of that name is installed.
var ErrNotInstalled = errors.New("no such plugin installed")

// List is the installed plugins, by name.
func (s *Store) List() ([]Installed, error) {
	var idx struct {
		Plugins []Installed `toml:"plugins"`
	}
	if _, err := toml.DecodeFile(filepath.Join(s.Dir, indexFile), &idx); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%s: %w", indexFile, err)
	}
	slices.SortFunc(idx.Plugins, func(a, b Installed) int { return strings.Compare(a.Name, b.Name) })
	return idx.Plugins, nil
}

func (s *Store) save(list []Installed) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	tmp := filepath.Join(s.Dir, indexFile+".tmp")
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	fmt.Fprintln(f, "# Blitz's installed plugins; change them with blitz plugin.")
	err = toml.NewEncoder(f).Encode(struct {
		Plugins []Installed `toml:"plugins"`
	}{list})
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(s.Dir, indexFile))
}

// Get is the installed plugin name.
func (s *Store) Get(name string) (Installed, error) {
	list, err := s.List()
	if err != nil {
		return Installed{}, err
	}
	for _, i := range list {
		if i.Name == name {
			return i, nil
		}
	}
	return Installed{}, fmt.Errorf("%w: %s", ErrNotInstalled, name)
}

// Staged is a plugin fetched for installing, not yet installed: look at
// it (Plugin.Summary), then Install or Discard it.
type Staged struct {
	*Plugin
	Source string
	tmp    string // removed by Discard
}

// Discard removes what Fetch downloaded.
func (st *Staged) Discard() {
	if st != nil && st.tmp != "" {
		os.RemoveAll(st.tmp)
	}
}

var gitSource = regexp.MustCompile(`^(git@|git://|ssh://)|\.git$|^https://(github\.com|gitlab\.com|codeberg\.org)/[^/]+/[^/]+/?$`)

// Fetch gets the plugin at src: a directory, a .zip file, an https URL of
// a .zip, or a git repository (cloned, the default branch unless
// src#ref).
func Fetch(ctx context.Context, src string) (*Staged, error) {
	st := &Staged{Source: src}
	var dir string
	switch {
	case gitSource.MatchString(strings.SplitN(src, "#", 2)[0]):
		tmp, err := os.MkdirTemp("", "blitz-plugin-")
		if err != nil {
			return nil, err
		}
		st.tmp = tmp
		repo, ref, _ := strings.Cut(src, "#")
		args := []string{"clone", "--quiet", "--depth", "1"}
		if ref != "" {
			args = append(args, "--branch", ref)
		}
		cmd := exec.CommandContext(ctx, "git", append(args, "--", repo, filepath.Join(tmp, "p"))...)
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			st.Discard()
			return nil, fmt.Errorf("git clone %s: %v: %s", repo, err, strings.TrimSpace(string(out)))
		}
		dir = filepath.Join(tmp, "p")
	case strings.HasPrefix(src, "https://") || strings.HasPrefix(src, "http://"):
		tmp, err := os.MkdirTemp("", "blitz-plugin-")
		if err != nil {
			return nil, err
		}
		st.tmp = tmp
		zipPath := filepath.Join(tmp, "p.zip")
		if err := download(ctx, src, zipPath); err != nil {
			st.Discard()
			return nil, err
		}
		if dir, err = unzip(zipPath, filepath.Join(tmp, "p")); err != nil {
			st.Discard()
			return nil, err
		}
	case strings.HasSuffix(strings.ToLower(src), ".zip"):
		tmp, err := os.MkdirTemp("", "blitz-plugin-")
		if err != nil {
			return nil, err
		}
		st.tmp = tmp
		if dir, err = unzip(src, filepath.Join(tmp, "p")); err != nil {
			st.Discard()
			return nil, err
		}
	default:
		abs, err := filepath.Abs(config.ExpandHome(src))
		if err != nil {
			return nil, err
		}
		dir = abs
		st.Source = abs
	}
	p, err := Read(dir)
	if err != nil {
		st.Discard()
		return nil, err
	}
	st.Plugin = p
	return st, nil
}

// maxDownload bounds a downloaded plugin.
const maxDownload = 64 << 20

func download(ctx context.Context, u, to string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading %s: HTTP %d", u, resp.StatusCode)
	}
	f, err := os.Create(to)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(resp.Body, maxDownload+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > maxDownload {
		err = fmt.Errorf("%s is larger than %d MB", u, maxDownload>>20)
	}
	return err
}

// unzip extracts the archive into dir and returns the plugin's root: dir,
// or the one folder the archive holds.
func unzip(archive, dir string) (string, error) {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return "", err
	}
	defer r.Close()
	var total uint64
	for _, f := range r.File {
		name := filepath.FromSlash(f.Name)
		if !filepath.IsLocal(name) {
			return "", fmt.Errorf("%s: %q leaves the archive", archive, f.Name)
		}
		dst := filepath.Join(dir, name)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return "", err
			}
			continue
		}
		if !f.Mode().IsRegular() {
			continue // no links
		}
		if total += f.UncompressedSize64; total > maxBytes {
			return "", fmt.Errorf("%s: larger than %d MB unpacked", archive, maxBytes>>20)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return "", err
		}
		if err := extract(f, dst); err != nil {
			return "", err
		}
	}
	if exists(filepath.Join(dir, Manifest)) {
		return dir, nil
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) == 1 && entries[0].IsDir() {
		return filepath.Join(dir, entries[0].Name()), nil
	}
	return dir, nil
}

func extract(f *zip.File, dst string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode().Perm()|0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, io.LimitReader(rc, maxBytes))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

// Install copies the staged plugin into the store, enabled, replacing
// another version of it, and records its hash.
func (s *Store) Install(st *Staged) (Installed, error) {
	list, err := s.List()
	if err != nil {
		return Installed{}, err
	}
	i := Installed{Name: st.Name, Version: st.Version, Source: st.Source, Enabled: true, Time: time.Now().UTC().Truncate(time.Second)}
	dst := s.PluginDir(i)
	staging := dst + ".new"
	os.RemoveAll(staging)
	if err := copyTree(st.Dir, staging); err != nil {
		os.RemoveAll(staging)
		return Installed{}, err
	}
	if i.Hash, err = Hash(staging); err != nil {
		os.RemoveAll(staging)
		return Installed{}, err
	}
	os.RemoveAll(dst)
	if err := os.Rename(staging, dst); err != nil {
		return Installed{}, err
	}
	for j, old := range list {
		if old.Name == i.Name {
			i.Enabled = old.Enabled
			if old.Version != i.Version {
				os.RemoveAll(s.PluginDir(old))
			}
			list = slices.Delete(list, j, j+1)
			break
		}
	}
	return i, s.save(append(list, i))
}

// SetEnabled turns an installed plugin on or off for every workspace.
func (s *Store) SetEnabled(name string, on bool) error {
	list, err := s.List()
	if err != nil {
		return err
	}
	for j := range list {
		if list[j].Name == name {
			list[j].Enabled = on
			return s.save(list)
		}
	}
	return fmt.Errorf("%w: %s", ErrNotInstalled, name)
}

// Remove deletes an installed plugin.
func (s *Store) Remove(name string) error {
	list, err := s.List()
	if err != nil {
		return err
	}
	for j, i := range list {
		if i.Name == name {
			if err := os.RemoveAll(filepath.Join(s.Dir, i.Name)); err != nil {
				return err
			}
			return s.save(slices.Delete(list, j, j+1))
		}
	}
	return fmt.Errorf("%w: %s", ErrNotInstalled, name)
}

// copyTree copies the regular files and directories under src to dst
// (links and .git are left out).
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(filepath.Join(dst, rel), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm()|0o600)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, in)
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		return err
	})
}
