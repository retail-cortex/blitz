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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
)

// NodeEnvs manages npm package environments for TypeScript skill scripts
// (BL-SK-01), one per distinct set of dependencies, in
// ~/.blitz/node-envs/<key>, as PyEnvs does for Python: built inside the
// script sandbox (network on, writes only to the environment and npm's
// cache), never running packages' install scripts, and used only once its
// marker file is written.
type NodeEnvs struct {
	dir      string
	registry string

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// NodeEnv describes one environment.
type NodeEnv struct {
	Key      string    `json:"key"`
	Node     string    `json:"node"` // the runtime (real path)
	Deps     []string  `json:"deps"`
	Registry string    `json:"registry"`
	Created  time.Time `json:"created"`
	LastUsed time.Time `json:"last_used"`
	Skills   []string  `json:"skills"`
	Dir      string    `json:"-"`
}

// Modules is the environment's node_modules.
func (e NodeEnv) Modules() string { return filepath.Join(e.Dir, "node_modules") }

// NewNodeEnvs manages environments in dir (default ~/.blitz/node-envs)
// under the npm package policy.
func NewNodeEnvs(dir string, p config.NPMPolicy) *NodeEnvs {
	if dir == "" {
		dir = config.ExpandHome("~/.blitz/node-envs")
	}
	return &NodeEnvs{dir: dir, registry: cmpOr(p.Registry, "https://registry.npmjs.org"), locks: map[string]*sync.Mutex{}}
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// minNode is the first Node that runs TypeScript by stripping its types.
var minNode = [3]int{22, 6, 0}

var nodeVersionRE = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)`)

// nodeVersion reads a `node --version`.
func nodeVersion(s string) ([3]int, bool) {
	m := nodeVersionRE.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return [3]int{}, false
	}
	var v [3]int
	for i := range v {
		v[i], _ = strconv.Atoi(m[i+1])
	}
	return v, true
}

// SystemNode is the node on PATH, as its real path, when it runs
// TypeScript (22.6 or later).
func SystemNode() (string, error) {
	p, err := exec.LookPath("node")
	if err != nil {
		return "", errors.New("node not found on PATH (TypeScript scripts need Node 22.6 or later)")
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", err
	}
	out, err := exec.Command(real, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("node --version: %w", err)
	}
	v, ok := nodeVersion(string(out))
	if !ok || slices.Compare(v[:], minNode[:]) < 0 {
		return "", fmt.Errorf("node %s can't run TypeScript: Node 22.6 or later is needed", strings.TrimSpace(string(out)))
	}
	return real, nil
}

// npmCLI is npm's own script for node (npm-cli.js), so installs run with
// the same node in the sandbox (npm's launcher looks node up on a PATH the
// sandbox doesn't give).
func npmCLI(node string) (string, error) {
	candidates := []string{filepath.Join(filepath.Dir(filepath.Dir(node)), "lib", "node_modules", "npm", "bin", "npm-cli.js")}
	if p, err := exec.LookPath("npm"); err == nil {
		if real, err := filepath.EvalSymlinks(p); err == nil && strings.HasSuffix(real, ".js") {
			candidates = append([]string{real}, candidates...)
		}
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", errors.New("npm not found beside node (TypeScript scripts with dependencies need npm)")
}

// Key identifies the environment for deps.
func (m *NodeEnvs) Key(node string, deps []string) string {
	norm := make([]string, len(deps))
	for i, d := range deps {
		norm[i] = strings.Join(strings.Fields(d), "")
	}
	sort.Strings(norm)
	b, _ := json.Marshal(struct {
		Node     string
		Deps     []string
		Registry string
	}{node, slices.Compact(norm), m.registry})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:16]
}

// Lookup returns the environment for deps and whether it's ready.
func (m *NodeEnvs) Lookup(node string, deps []string) (NodeEnv, bool) {
	key := m.Key(node, deps)
	if e, err := m.read(key); err == nil {
		return e, true
	}
	return NodeEnv{Key: key, Dir: filepath.Join(m.dir, key), Node: node, Deps: deps, Registry: m.registry}, false
}

// InstallCommand describes how Ensure installs deps, for the approval.
func (m *NodeEnvs) InstallCommand(deps []string) string {
	return fmt.Sprintf("npm install --ignore-scripts --registry %s -- %s", m.registry, strings.Join(deps, " "))
}

// Ensure returns the environment for deps, building it in box first if
// needed (the caller has approved the install).
func (m *NodeEnvs) Ensure(ctx context.Context, box ScriptBox, node, skill string, deps []string) (NodeEnv, error) {
	key := m.Key(node, deps)
	lock := m.lock(key)
	lock.Lock()
	defer lock.Unlock()
	if e, err := m.read(key); err == nil {
		return m.touch(e, skill), nil
	}
	e := NodeEnv{Key: key, Dir: filepath.Join(m.dir, key), Node: node, Deps: slices.Clone(deps), Registry: m.registry, Created: time.Now()}
	if err := os.RemoveAll(e.Dir); err != nil { // an interrupted build
		return e, err
	}
	cache := filepath.Join(m.dir, pyEnvCacheDir)
	for _, d := range []string{e.Dir, cache} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return e, err
		}
	}
	if err := m.build(ctx, box, e, cache); err != nil {
		os.RemoveAll(e.Dir)
		return e, err
	}
	return m.touch(e, skill), nil
}

func (m *NodeEnvs) build(ctx context.Context, box ScriptBox, e NodeEnv, cache string) error {
	cli, err := npmCLI(e.Node)
	if err != nil {
		return err
	}
	// A package.json of its own, so npm installs here and nowhere above.
	if err := os.WriteFile(filepath.Join(e.Dir, "package.json"), []byte(`{"name":"blitz-skill-env","private":true}`+"\n"), 0o600); err != nil {
		return err
	}
	argv := append([]string{e.Node, cli, "install", "--ignore-scripts", "--no-audit", "--no-fund", "--no-update-notifier",
		"--registry", e.Registry, "--cache", cache, "--prefix", e.Dir, "--"}, e.Deps...)
	ctx, cancel := context.WithTimeout(ctx, pyEnvBuildLimit)
	defer cancel()
	var out bytes.Buffer
	res, err := box.Run(ctx, ScriptRequest{
		Argv: argv, Dir: e.Dir, Env: []string{"npm_config_update_notifier=false"}, Network: true,
		ReadOnly: append(MountsFor(e.Node), filepath.Dir(filepath.Dir(cli))), Writable: []string{e.Dir, cache},
		Stdout: &out, Stderr: &out,
	})
	switch {
	case err != nil:
		return fmt.Errorf("npm: %w", err)
	case res.TimedOut:
		return fmt.Errorf("installing packages took longer than %s", pyEnvBuildLimit)
	case res.ExitCode != 0:
		return fmt.Errorf("npm install failed (exit %d): %s", res.ExitCode, lastLines(out.String(), 12))
	}
	return m.writeMarker(e)
}

func (m *NodeEnvs) lock(key string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.locks[key] == nil {
		m.locks[key] = &sync.Mutex{}
	}
	return m.locks[key]
}

func (m *NodeEnvs) read(key string) (NodeEnv, error) {
	var e NodeEnv
	b, err := os.ReadFile(filepath.Join(m.dir, key, pyEnvMarker))
	if err != nil {
		return e, err
	}
	if err := json.Unmarshal(b, &e); err != nil {
		return e, err
	}
	e.Dir = filepath.Join(m.dir, key)
	return e, nil
}

func (m *NodeEnvs) touch(e NodeEnv, skill string) NodeEnv {
	e.LastUsed = time.Now()
	if skill != "" && !slices.Contains(e.Skills, skill) {
		e.Skills = append(e.Skills, skill)
	}
	_ = m.writeMarker(e)
	return e
}

func (m *NodeEnvs) writeMarker(e NodeEnv) error {
	b, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(e.Dir, pyEnvMarker+".tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(e.Dir, pyEnvMarker))
}
