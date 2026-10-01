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
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ExecEnv builds every process Blitz runs on the model's behalf: shell
// commands, background processes, and forged tools. Each process is placed
// in its own process group, optionally wrapped in the OS sandbox, and guarded
// so the group is killed if Blitz exits for any reason.
type ExecEnv struct {
	Sandbox *OSSandbox
	// ScrubEnv lists environment variable names (globs allowed, e.g.
	// "*_API_KEY") removed from every child process, so commands the model
	// runs can't read Blitz's own credentials.
	ScrubEnv []string
	// Dir is where commands run unless they set their own directory: the
	// workspace root, never the process's working directory.
	Dir string
	// Credentials are files each process gets its own copy of, named by an
	// environment variable, while the originals stay blocked: the sign-in
	// Blitz itself uses (Google's ADC), for commands that need it too.
	Credentials []Credential
}

// Credential is a file a process gets a copy of: Path gives the file as
// the process starts ("" for none), and Env is set to the copy's path.
type Credential struct {
	Env  string
	Path func() string
}

// credentialsDir holds the copies, one directory per process, removed
// when it ends. It's in the user's cache directory, which sandboxed
// commands can read. Tests replace it.
var credentialsDir = func() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "blitz", "credentials"), nil
}

// staleCredentials is how long a copy left by a process that outlived
// Blitz (it crashed) stays before sweepCredentials removes it.
const staleCredentials = 24 * time.Hour

// copyCredentials copies each credential into a new directory and returns
// the variables naming the copies, and a function that removes them.
func (e *ExecEnv) copyCredentials() ([]string, func(), error) {
	var env []string
	var dir string
	remove := func() {
		if dir != "" {
			os.RemoveAll(dir)
		}
	}
	for _, c := range e.Credentials {
		src := c.Path()
		if src == "" {
			continue
		}
		data, err := os.ReadFile(src)
		if err != nil {
			continue // not signed in (yet): the command runs without it
		}
		if dir == "" {
			root, err := credentialsDir()
			if err != nil {
				return nil, remove, err
			}
			if err := os.MkdirAll(root, 0o700); err != nil {
				return nil, remove, err
			}
			if dir, err = os.MkdirTemp(root, "run-"); err != nil {
				return nil, remove, err
			}
		}
		dst := filepath.Join(dir, strings.ToLower(c.Env)+filepath.Ext(src))
		if err := os.WriteFile(dst, data, 0o600); err != nil {
			remove()
			return nil, func() {}, err
		}
		env = append(env, c.Env+"="+dst)
	}
	return env, remove, nil
}

// sweepCredentials removes copies older than staleCredentials.
func sweepCredentials() {
	root, err := credentialsDir()
	if err != nil {
		return
	}
	entries, _ := os.ReadDir(root)
	for _, d := range entries {
		if info, err := d.Info(); err == nil && time.Since(info.ModTime()) > staleCredentials {
			os.RemoveAll(filepath.Join(root, d.Name()))
		}
	}
}

// guardedCmd is an exec.Cmd whose process group dies with Blitz.
type guardedCmd struct {
	*exec.Cmd
	childEnd *os.File
	once     sync.Once
	release  func()
}

// command prepares argv to run under ctx. A nil ExecEnv runs unsandboxed.
func (e *ExecEnv) command(ctx context.Context, argv []string) (*guardedCmd, error) {
	if e != nil {
		argv = e.Sandbox.wrap(argv)
	}
	wrapped, childEnd, release, err := guardArgv(argv)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, wrapped[0], wrapped[1:]...)
	if e != nil {
		cmd.Dir = e.Dir
	}
	if childEnd != nil {
		cmd.ExtraFiles = []*os.File{childEnd}
	}
	if e != nil && len(e.ScrubEnv) > 0 {
		cmd.Env = scrubEnv(os.Environ(), e.ScrubEnv)
	}
	if e != nil && len(e.Credentials) > 0 {
		env, remove, err := e.copyCredentials()
		if err != nil {
			release()
			if childEnd != nil {
				childEnd.Close()
			}
			return nil, fmt.Errorf("copying the sign-in's credentials: %w", err)
		}
		if len(env) > 0 {
			if cmd.Env == nil {
				cmd.Env = os.Environ()
			}
			cmd.Env = append(cmd.Env, env...)
		}
		guard := release
		release = func() {
			guard()
			remove()
		}
	}
	configureProcessGroup(cmd)
	cmd.WaitDelay = shellWaitDelay
	return &guardedCmd{Cmd: cmd, childEnd: childEnd, release: release}, nil
}

// Start starts the command and drops the parent's copy of the child's guard fd.
func (g *guardedCmd) Start() error {
	err := g.Cmd.Start()
	if g.childEnd != nil {
		g.childEnd.Close()
	}
	if err != nil {
		g.Release()
	}
	return err
}

// Wait waits for the command, then releases the guard, which kills anything
// the command left running in its process group.
func (g *guardedCmd) Wait() error {
	err := g.Cmd.Wait()
	g.Release()
	return err
}

// Run starts the command and waits for it.
func (g *guardedCmd) Run() error {
	if err := g.Start(); err != nil {
		return err
	}
	return g.Wait()
}

// Release closes the guard pipe; safe to call more than once.
func (g *guardedCmd) Release() { g.once.Do(g.release) }

// scrubEnv drops variables whose names match any pattern (case-insensitive).
func scrubEnv(env, patterns []string) []string {
	out := env[:0:0]
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		drop := false
		for _, p := range patterns {
			if ok, _ := path.Match(strings.ToUpper(p), strings.ToUpper(name)); ok {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, kv)
		}
	}
	return out
}
