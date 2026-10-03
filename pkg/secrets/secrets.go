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

// Package secrets keeps Blitz's secrets (API keys) out of its settings
// files: in the macOS Keychain, in the Secret Service on Linux (GNOME
// Keyring, KWallet), or, where neither is available, in an owner-only file
// beside the settings. A settings file refers to a stored secret as
// "keychain:<name>" (Ref); config resolves the reference when it loads.
package secrets

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
)

// Service is the name secrets are stored under (the keychain item's
// service, the Secret Service attribute).
const Service = "dev.blitz"

// RefPrefix marks a settings value that names a stored secret.
const RefPrefix = "keychain:"

// ErrNotFound means no secret is stored under the name.
var ErrNotFound = errors.New("no such secret")

// Store keeps secrets by name.
type Store interface {
	// Kind says where secrets go, for people ("macOS Keychain").
	Kind() string
	Get(name string) (string, error)
	Set(name, value string) error
	Delete(name string) error
}

// Ref is the settings value that names the secret name.
func Ref(name string) string { return RefPrefix + name }

// ParseRef returns the secret a settings value names, if it names one.
func ParseRef(v string) (string, bool) {
	if !strings.HasPrefix(v, RefPrefix) || len(v) == len(RefPrefix) {
		return "", false
	}
	return v[len(RefPrefix):], true
}

var (
	defaultOnce  sync.Once
	defaultStore Store
)

// Default is this machine's store: the Keychain on macOS, the Secret
// Service on Linux when secret-tool can reach one, else a file in dir
// (the settings directory).
func Default(dir string) Store {
	defaultOnce.Do(func() {
		switch {
		case goruntime.GOOS == "darwin":
			defaultStore = keychain{}
		case goruntime.GOOS == "linux" && secretServiceAvailable():
			defaultStore = secretService{}
		default:
			defaultStore = &FileStore{Path: filepath.Join(dir, "secrets.toml")}
		}
	})
	return defaultStore
}

// SetDefault replaces the store Default returns (tests, and the service
// once it knows its settings directory).
func SetDefault(s Store) {
	defaultOnce.Do(func() {})
	defaultStore = s
}

// ---- macOS: the Keychain, through /usr/bin/security.

type keychain struct{}

// security is the Keychain's command-line tool (a variable for tests).
var security = "/usr/bin/security"

func (keychain) Kind() string { return "macOS Keychain" }

func (keychain) Get(name string) (string, error) {
	out, err := exec.Command(security, "find-generic-password", "-s", Service, "-a", name, "-w").Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 44 { // errSecItemNotFound
			return "", ErrNotFound
		}
		return "", fmt.Errorf("reading %s from the Keychain: %w", name, err)
	}
	return strings.TrimSuffix(string(out), "\n"), nil
}

// Set writes through `security -i`, the command on stdin: the secret
// never appears in a process's arguments. -X takes it hex-encoded.
func (keychain) Set(name, value string) error {
	cmd := exec.Command(security, "-i")
	cmd.Stdin = strings.NewReader(fmt.Sprintf("add-generic-password -U -s %s -a %s -l %s -X %s\n",
		quote(Service), quote(name), quote("Blitz: "+name), hex.EncodeToString([]byte(value))))
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil || strings.Contains(out.String(), "error") {
		return fmt.Errorf("saving %s in the Keychain: %v %s", name, err, strings.TrimSpace(out.String()))
	}
	return nil
}

func (keychain) Delete(name string) error {
	err := exec.Command(security, "delete-generic-password", "-s", Service, "-a", name).Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 44 {
		return nil // already gone
	}
	return err
}

// quote quotes s for `security -i`'s command line.
func quote(s string) string { return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"` }

// ---- Linux: the Secret Service, through secret-tool (libsecret).

type secretService struct{}

func secretServiceAvailable() bool {
	if _, err := exec.LookPath("secret-tool"); err != nil {
		return false
	}
	// A lookup of nothing succeeds or fails cleanly only when a service
	// answers on the session bus.
	cmd := exec.Command("secret-tool", "search", "service", Service+".probe")
	return cmd.Run() == nil || isExit(cmd, 1)
}

func isExit(cmd *exec.Cmd, code int) bool {
	return cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == code
}

func (secretService) Kind() string { return "Secret Service" }

func (secretService) Get(name string) (string, error) {
	cmd := exec.Command("secret-tool", "lookup", "service", Service, "account", name)
	out, err := cmd.Output()
	if err != nil {
		if isExit(cmd, 1) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("reading %s from the Secret Service: %w", name, err)
	}
	return strings.TrimSuffix(string(out), "\n"), nil
}

// Set gives secret-tool the secret on stdin, never as an argument.
func (secretService) Set(name, value string) error {
	cmd := exec.Command("secret-tool", "store", "--label=Blitz: "+name, "service", Service, "account", name)
	cmd.Stdin = strings.NewReader(value)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("saving %s in the Secret Service: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (secretService) Delete(name string) error {
	cmd := exec.Command("secret-tool", "clear", "service", Service, "account", name)
	if err := cmd.Run(); err != nil && !isExit(cmd, 1) {
		return err
	}
	return nil
}

// ---- Elsewhere: an owner-only file (0600, in a 0700 directory). It
// keeps secrets out of the settings file, which may be shared, but on
// disk they're only as safe as the file's permissions.

// FileStore keeps secrets in a TOML file.
type FileStore struct {
	Path string
	mu   sync.Mutex
}

// Kind names the store and its file.
func (f *FileStore) Kind() string { return "file (" + f.Path + ")" }

func (f *FileStore) read() (map[string]string, error) {
	m := map[string]string{}
	if _, err := toml.DecodeFile(f.Path, &m); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return m, nil
}

func (f *FileStore) write(m map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o700); err != nil {
		return err
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("# Blitz's secrets (no OS keychain was available). Keep this file private.\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "%q = %q\n", k, m[k])
	}
	tmp, err := os.CreateTemp(filepath.Dir(f.Path), ".secrets.*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(b.String()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), f.Path)
}

// Get returns the secret stored under name, or ErrNotFound.
func (f *FileStore) Get(name string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.read()
	if err != nil {
		return "", err
	}
	v, ok := m[name]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

// Set stores value under name, rewriting the file atomically.
func (f *FileStore) Set(name, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.read()
	if err != nil {
		return err
	}
	m[name] = value
	return f.write(m)
}

// Delete removes name's secret; a missing one isn't an error.
func (f *FileStore) Delete(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.read()
	if err != nil {
		return err
	}
	if _, ok := m[name]; !ok {
		return nil
	}
	delete(m, name)
	return f.write(m)
}

// Memory keeps secrets in memory, for tests.
type Memory struct {
	mu sync.Mutex
	m  map[string]string
}

// Kind is "memory".
func (s *Memory) Kind() string { return "memory" }

// Get returns the secret stored under name, or ErrNotFound.
func (s *Memory) Get(name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[name]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

// Set stores value under name.
func (s *Memory) Set(name, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string]string{}
	}
	s.m[name] = value
	return nil
}

// Delete removes name's secret; a missing one isn't an error.
func (s *Memory) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, name)
	return nil
}
