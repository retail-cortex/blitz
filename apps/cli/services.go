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

package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/pkg/loginitem"
	"github.com/retail-cortex/blitz/pkg/socket"
)

// The CLI runs no workspace itself: every one is in a Blitz service
// (blitzd). That's the per-user service, started on demand when it isn't
// running, or, for a run whose settings are its own (--local,
// --append-system-prompt, --add-dir, --plugin-dir, --no-session-persistence,
// --trust-project), a private service started for the run alone.

// sharedIdle is how long a service the CLI started waits for a client
// before it stops (a login item's never does); a variable for tests.
var sharedIdle = 15 * time.Minute

// startWait is how long the CLI waits for a service to answer; a variable
// for tests.
var startWait = 15 * time.Second

// serviceOptions are a private service's settings: the CLI's configuration
// file, and this run's settings over it.
type serviceOptions struct {
	Config               string
	Model, Agent, Agency string
	PluginDirs, AddDirs  []string
	SessionDir           string
	TrustProject         bool
	AppendSystemPrompt   string
}

// args are o as blitzd's flags, its prompt written into dir.
func (o serviceOptions) args(dir string) ([]string, error) {
	var a []string
	add := func(flag, v string) {
		if v != "" {
			a = append(a, flag, v)
		}
	}
	add("--config", o.Config)
	add("--model", o.Model)
	add("--agent", o.Agent)
	add("--agency", o.Agency)
	for _, d := range o.PluginDirs {
		add("--plugin-dir", d)
	}
	for _, d := range o.AddDirs {
		add("--add-dir", d)
	}
	add("--session-dir", o.SessionDir)
	if o.TrustProject {
		a = append(a, "--trust-project")
	}
	if o.AppendSystemPrompt != "" { // a file: it may be long, and argv is public
		p := filepath.Join(dir, "prompt.md")
		if err := os.WriteFile(p, []byte(o.AppendSystemPrompt), 0o600); err != nil {
			return nil, err
		}
		add("--append-system-prompt-file", p)
	}
	return a, nil
}

// ensureService returns the per-user service's socket, starting the
// service first when nothing answers there. Clients starting it at the
// same moment take turns (startLock): one starts it, the others find it
// answering.
func ensureService(ctx context.Context) (string, error) {
	sock := socket.DefaultSocket()
	if socket.Running(sock) {
		return sock, nil
	}
	unlock, err := startLock(ctx, sock)
	if err != nil {
		return "", fmt.Errorf("starting the Blitz service: %w", err)
	}
	defer unlock()
	if socket.Running(sock) { // another client started it while this one waited
		return sock, nil
	}
	if err := startShared(ctx, sock); err != nil {
		return "", fmt.Errorf("starting the Blitz service: %w", err)
	}
	return sock, nil
}

// staleLock is how old a start lock is when its holder must have died.
var staleLock = 2 * startWait

// startLock takes the lock that lets one client at a time start the
// service on sock: a file beside the socket, created exclusively and
// removed by the returned func. It waits while another client holds it,
// returning at once when the service answers meanwhile, and takes over a
// lock older than staleLock (its holder died, or is stuck): so it waits at
// most that long.
func startLock(ctx context.Context, sock string) (func(), error) {
	path := sock + ".start"
	if err := os.MkdirAll(filepath.Dir(sock), 0o700); err != nil {
		return nil, err
	}
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			f.Close()
			return func() { os.Remove(path) }, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) > staleLock {
			os.Remove(path) // its holder died
			continue
		}
		if socket.Running(sock) {
			return func() {}, nil // the holder started it
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// serviceOutput is where a started service writes its own output: a file,
// never a pipe to this process, which a service that outlives it would
// die writing to (SIGPIPE). read is what it wrote; done closes and
// removes the file (the service keeps writing to it, unlinked, if it
// runs on).
func serviceOutput() (f *os.File, read func() string, done func(), err error) {
	f, err = os.CreateTemp("", "blitzd-*.log")
	if err != nil {
		return nil, nil, nil, err
	}
	read = func() string {
		data, _ := os.ReadFile(f.Name())
		return string(data)
	}
	done = func() {
		f.Close()
		os.Remove(f.Name())
	}
	return f, read, done, nil
}

// serviceDir is where a started service runs: the home folder, so it
// doesn't keep the folder blitz ran in busy.
func serviceDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return os.TempDir()
	}
	return home
}

// startShared starts the per-user service on sock: its login item when
// it has one, else blitzd (beside this blitz, else on PATH) on its own,
// stopping after sharedIdle without clients. Tests replace it.
var startShared = func(ctx context.Context, sock string) error {
	if loginitem.Installed() && sock == socket.DefaultSocket() {
		if err := loginitem.Start(); err != nil {
			return err
		}
		return waitService(ctx, sock, nil)
	}
	bin, err := serviceBinary()
	if err != nil {
		return err
	}
	cmd := exec.Command(bin, "--socket", sock, "--idle-exit", sharedIdle.String())
	cmd.SysProcAttr = detached() // it outlives this blitz
	cmd.Dir = serviceDir()
	out, read, closeOut, err := serviceOutput()
	if err != nil {
		return err
	}
	defer closeOut()
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	if err := waitService(ctx, sock, done); err != nil {
		if socket.Running(sock) { // another client started it meanwhile
			return nil
		}
		return withStderr(err, read())
	}
	return nil
}

// private are the private services this process started, kept until it
// exits (their stdin pipes close then, and they stop).
var private struct {
	mu   sync.Mutex
	list []*privateService
}

// privateService is a service for this run alone: stop stops it.
type privateService struct {
	stop func()
}

// keepPrivate keeps a private service until stopPrivate.
func keepPrivate(stop func()) {
	private.mu.Lock()
	private.list = append(private.list, &privateService{stop: stop})
	private.mu.Unlock()
}

// startPrivate starts a service for this run alone, with o, on a socket in
// a folder of its own, and returns the socket. It stops when this process
// does, however that happens (its stdin is a pipe from here), or at
// stopPrivate. Tests replace it.
var startPrivate = func(ctx context.Context, o serviceOptions) (string, error) {
	bin, err := serviceBinary()
	if err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", "blitz-")
	if err != nil {
		return "", err
	}
	sock := filepath.Join(dir, "s.sock")
	args, err := o.args(dir)
	if err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	cmd := exec.Command(bin, append([]string{"--socket", sock, "--exit-with-stdin"}, args...)...)
	cmd.Dir = serviceDir()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	out, read, closeOut, err := serviceOutput()
	if err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	defer closeOut()
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	keepPrivate(func() {
		stdin.Close() // the service stops
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			cmd.Process.Kill()
			<-done
		}
		os.RemoveAll(dir)
	})
	if err := waitService(ctx, sock, done); err != nil {
		return "", withStderr(err, read())
	}
	os.Remove(filepath.Join(dir, "prompt.md")) // read before it answered: not left behind
	return sock, nil
}

// stopPrivate stops the private services and removes their folders.
func stopPrivate() {
	private.mu.Lock()
	list := private.list
	private.list = nil
	private.mu.Unlock()
	for _, ps := range list {
		ps.stop()
	}
}

// waitService waits for a service to answer on sock, for up to startWait;
// exited, when given, is closed if the service stops first.
func waitService(ctx context.Context, sock string, exited <-chan struct{}) error {
	deadline := time.Now().Add(startWait)
	for !socket.Running(sock) {
		select {
		case <-exited:
			return errors.New("blitzd stopped before answering")
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("blitzd didn't answer on %s within %s", sock, startWait)
		}
	}
	return nil
}

// withStderr adds what the service printed to err.
func withStderr(err error, printed string) error {
	if s := strings.TrimSpace(printed); s != "" {
		return fmt.Errorf("%w: %s", err, s)
	}
	return err
}
