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

package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/skills/builtin"
)

// Provider discovers, indexes, and activates skills across embedded and external paths.
type Provider struct {
	mu      sync.RWMutex
	skills  map[string]*Skill
	builtin map[string]bool
	// dirs are the folders searched, and stamps what they held when last
	// searched: a skill added, edited, replaced or removed (by a registry
	// install, /skills update, the agent, an editor) changes its stamp,
	// and the next look searches again (SK-95).
	dirs   []string
	stamps map[string]string
}

// NewProvider initializes the skills provider with embedded built-in skills.
func NewProvider() (*Provider, error) {
	p := &Provider{
		skills:  make(map[string]*Skill),
		builtin: make(map[string]bool),
	}

	if err := p.loadEmbeddedSkills(); err != nil {
		return nil, fmt.Errorf("failed to load embedded skills: %w", err)
	}

	return p, nil
}

// Builtins are the skills Blitz ships with, by name: what every workspace
// has before it finds any of its own (Settings › Skills lists them).
func Builtins() ([]*Skill, error) {
	p, err := NewProvider()
	if err != nil {
		return nil, err
	}
	return p.listLocked(), nil
}

func (p *Provider) loadEmbeddedSkills() error {
	err := fs.WalkDir(builtin.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "SKILL.md" {
			return nil
		}

		data, err := builtin.FS.ReadFile(path)
		if err != nil {
			return err
		}

		skill, err := ParseSkillMD(data, "builtin://"+path)
		if err == nil && skill.Name != "" {
			skill.fsys, skill.root = builtin.FS, filepath.ToSlash(filepath.Dir(path))
			skill.scanBundle()
			p.skills[skill.Name] = skill
			p.builtin[skill.Name] = true
		}
		return nil
	})

	return err
}

// DiscoverExternal scans search directories for SKILL.md files. External
// skills cannot replace built-in ones; conflicts are reported in the returned
// (joined) error while other skills still load.
func (p *Provider) DiscoverExternal(dirs []string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, d := range dirs {
		if !slices.Contains(p.dirs, d) {
			p.dirs = append(p.dirs, d)
		}
	}
	err := p.discover(dirs)
	p.stamps = stamps(p.dirs)
	return err
}

// stamps are what each folder holds, as discover reads it: every SKILL.md's
// path, size and time, and its folder's time (a resource added or removed
// beside it). Nothing else in a skill is looked at, so a skill holding an
// environment doesn't slow the look. A missing folder stamps as "".
func stamps(dirs []string) map[string]string {
	out := make(map[string]string, len(dirs))
	for _, d := range dirs {
		out[d] = folderStamp(config.ExpandHome(d))
	}
	return out
}

// heavyDirs are folders a skill may keep an environment or a repository
// in, which hold no skill: each turn's look skips them.
var heavyDirs = []string{".git", "node_modules", ".venv", "venv", "__pycache__"}

func folderStamp(dir string) string {
	if _, err := os.Stat(dir); err != nil {
		return ""
	}
	h := sha256.New()
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() && path != dir && slices.Contains(heavyDirs, d.Name()) {
			return filepath.SkipDir
		}
		if err != nil || d.IsDir() || d.Name() != "SKILL.md" {
			return nil
		}
		if info, err := os.Stat(path); err == nil {
			fmt.Fprintf(h, "%s\x00%d\x00%d", path, info.Size(), info.ModTime().UnixNano())
		}
		if info, err := os.Stat(filepath.Dir(path)); err == nil {
			fmt.Fprintf(h, "\x00%d", info.ModTime().UnixNano())
		}
		h.Write([]byte{'\n'})
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))
}

// Refresh searches the folders again when what they hold changed since it
// last did, so skills installed, edited or removed meanwhile come and go
// without a restart, and reports whether it did. Their problems were
// reported when the workspace opened; doctor reports a new one.
func (p *Provider) Refresh() bool {
	p.mu.RLock()
	if len(p.dirs) == 0 {
		p.mu.RUnlock()
		return false
	}
	same := maps.Equal(stamps(p.dirs), p.stamps)
	p.mu.RUnlock()
	if same {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := stamps(p.dirs)
	if maps.Equal(now, p.stamps) {
		return false // another look did it
	}
	p.rediscover(now)
	return true
}

// Reload searches the folders again whether or not they look changed
// (/reload), and returns what couldn't be loaded.
func (p *Provider) Reload() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.rediscover(stamps(p.dirs))
}

// rediscover drops the external skills and searches the folders again,
// which now holds; p.mu is held.
func (p *Provider) rediscover(now map[string]string) error {
	for name := range p.skills {
		if !p.builtin[name] {
			delete(p.skills, name)
		}
	}
	err := p.discover(p.dirs)
	p.stamps = now
	return err
}

// refresh is Refresh for a look at the skills.
func (p *Provider) refresh() { p.Refresh() }

// External counts the skills found in the folders (not built in).
func (p *Provider) External() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	n := 0
	for name := range p.skills {
		if !p.builtin[name] {
			n++
		}
	}
	return n
}

// discover adds the skills in dirs; p.mu is held.
func (p *Provider) discover(dirs []string) error {
	var errs []error
	for _, dir := range dirs {
		expandedDir := config.ExpandHome(dir)

		if _, err := os.Stat(expandedDir); os.IsNotExist(err) {
			continue
		}

		_ = filepath.WalkDir(expandedDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || d.Name() != "SKILL.md" {
				return nil
			}

			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}

			skill, err := ParseSkillMD(data, path)
			if err != nil {
				// Reported rather than skipped silently: a typo in the
				// frontmatter shouldn't make a skill vanish without a word.
				errs = append(errs, fmt.Errorf("%s: %w", path, err))
				return nil
			}
			if p.builtin[skill.Name] {
				errs = append(errs, fmt.Errorf("%s: skill name %q is reserved by a built-in skill", path, skill.Name))
				return nil
			}
			if skill.Name != "" {
				// Its resources, references and scripts, from its folder.
				skillDir := filepath.Dir(path)
				skill.fsys, skill.root, skill.hostDir = os.DirFS(skillDir), ".", skillDir
				skill.scanBundle()
				p.skills[skill.Name] = skill
			}
			return nil
		})
	}

	return errors.Join(errs...)
}

// IsBuiltin reports whether name is a built-in skill's, which a skill on
// disk can't take.
func (p *Provider) IsBuiltin(name string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.builtin[name]
}

// List returns all discovered skills sorted by name.
func (p *Provider) List() []*Skill {
	p.refresh()
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.listLocked()
}

// listLocked requires p.mu to be held. Search uses it rather than List because
// re-acquiring an RWMutex read lock can deadlock when a writer is waiting.
func (p *Provider) listLocked() []*Skill {
	result := make([]*Skill, 0, len(p.skills))
	for _, skill := range p.skills {
		result = append(result, skill)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}

// Search filters skills matching query in name, description, or tags.
func (p *Provider) Search(query string) []*Skill {
	p.refresh()
	p.mu.RLock()
	defer p.mu.RUnlock()

	if query == "" {
		return p.listLocked()
	}

	matches := []*Skill{}
	for _, skill := range p.skills {
		if skill.Matches(query) {
			matches = append(matches, skill)
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Name < matches[j].Name
	})

	return matches
}

// Get returns a skill by name.
func (p *Provider) Get(name string) (*Skill, bool) {
	p.refresh()
	p.mu.RLock()
	defer p.mu.RUnlock()

	s, ok := p.skills[name]
	return s, ok
}
