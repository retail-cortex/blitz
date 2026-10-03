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

// Package sandboxsetup is Linux's OS sandbox, from outside the engine:
// whether bubblewrap can run here, and, where AppArmor's restriction on
// unprivileged user namespaces stops it (Ubuntu 24.04 and later), an
// AppArmor profile that lets bwrap alone have them, installed with the
// user's password (sudo or pkexec). The CLI (blitz security) and the
// desktop app's settings use it.
package sandboxsetup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
)

// State is what Check found.
type State string

// The states.
const (
	// Unsupported means the OS isn't Linux: its sandbox (Seatbelt on macOS)
	// needs no set-up, or it has none.
	Unsupported State = "unsupported"
	// Ready means bubblewrap runs.
	Ready State = "ready"
	// NoBwrap means bubblewrap isn't installed.
	NoBwrap State = "no_bwrap"
	// Restricted means AppArmor's restriction on unprivileged user namespaces
	// stops bubblewrap, and Fix can lift it for bwrap alone.
	Restricted State = "restricted"
	// Broken means bubblewrap fails for another reason (a container's seccomp
	// profile, a kernel without user namespaces) that Fix can't help.
	Broken State = "broken"
)

// Status is the sandbox's state here, with the bwrap it found and why it
// fails, if it does.
type Status struct {
	State  State  `json:"state"`
	Bwrap  string `json:"bwrap,omitempty"`  // its real path
	Detail string `json:"detail,omitempty"` // bwrap's error
	// Profile is where Fix writes (or wrote) the profile.
	Profile string `json:"profile,omitempty"`
}

// The system it looks at, replaced in tests.
var (
	goos         = goruntime.GOOS
	lookPath     = exec.LookPath
	restrictFile = "/proc/sys/kernel/apparmor_restrict_unprivileged_userns"
	profileDir   = "/etc/apparmor.d"
	parsers      = []string{"/usr/sbin/apparmor_parser", "/sbin/apparmor_parser"}
	// probe runs bwrap the way the engine's sandbox does, returning its
	// output when it fails.
	probe = func(ctx context.Context, bwrap string) ([]byte, error) {
		return exec.CommandContext(ctx, bwrap, "--ro-bind", "/", "/", "--dev", "/dev", "--unshare-net", "true").CombinedOutput()
	}
)

// ProfileName is the AppArmor profile Fix installs, and its file's name in
// /etc/apparmor.d: Blitz's own, so a distribution's bwrap profile stays.
const ProfileName = "blitz-bwrap"

// Check reports whether bubblewrap runs here and, if not, whether Fix
// can make it.
func Check(ctx context.Context) Status {
	if goos != "linux" {
		return Status{State: Unsupported}
	}
	st := Status{Profile: filepath.Join(profileDir, ProfileName)}
	bwrap, err := lookPath("bwrap")
	if err != nil {
		st.State = NoBwrap
		return st
	}
	if real, err := filepath.EvalSymlinks(bwrap); err == nil {
		bwrap = real // AppArmor attaches profiles by the real path
	}
	st.Bwrap = bwrap
	out, err := probe(ctx, bwrap)
	if err == nil {
		st.State = Ready
		return st
	}
	st.Detail = strings.TrimSpace(string(out))
	if st.Detail == "" {
		st.Detail = err.Error()
	}
	st.State = Broken
	if restricted() && parser() != "" {
		st.State = Restricted
	}
	return st
}

// restricted reports whether AppArmor restricts unprivileged user
// namespaces.
func restricted() bool {
	b, err := os.ReadFile(restrictFile)
	return err == nil && strings.TrimSpace(string(b)) == "1"
}

// parser is apparmor_parser's path, or "".
func parser() string {
	for _, p := range parsers {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}

// Profile is the AppArmor profile that lets bwrap (its real path) create
// user namespaces, unconfined otherwise: what Ubuntu ships for programs
// that need them (/etc/apparmor.d/devhelp, say), for bwrap alone, while
// every other program stays restricted.
func Profile(bwrap string) string {
	return fmt.Sprintf(`# Written by Blitz (blitz security fix-apparmor): lets bubblewrap, the
# sandbox for the agent's commands, create user namespaces, which
# AppArmor otherwise restricts for unprivileged programs.
abi <abi/4.0>,
include <tunables/global>

profile %s %s flags=(unconfined) {
  userns,

  include if exists <local/%s>
}
`, ProfileName, bwrap, ProfileName)
}

// Elevation is how Fix runs its one privileged step: Command is sudo (in a
// terminal, which asks for the password there) or pkexec (the desktop's
// password dialog). Stdin, Stdout and Stderr are the terminal's, for sudo.
type Elevation struct {
	Command        string
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	// Graphical uses pkexec only: without a terminal, sudo can't ask.
	Graphical bool
}

// ErrNoElevation means neither sudo nor pkexec is installed.
var ErrNoElevation = errors.New("neither sudo nor pkexec is installed")

// ErrCantFix means the sandbox's problem isn't one Fix can help.
var ErrCantFix = errors.New("not something an AppArmor profile fixes")

// install is the privileged step: copy the profile into place, readable by
// all and owned by root, and load it. Its arguments are the profile's
// temporary copy, where it goes, and apparmor_parser.
const install = `install -m 0644 -o root -g root "$1" "$2" && "$3" -r "$2"`

// run runs a command, replaced in tests.
var run = func(cmd *exec.Cmd) error { return cmd.Run() }

// Fix installs and loads the profile with one password prompt (el), then
// checks again. It does nothing when the sandbox works, and fails with
// ErrCantFix when its problem isn't AppArmor's restriction.
func Fix(ctx context.Context, el Elevation) (Status, error) {
	st := Check(ctx)
	switch st.State {
	case Ready:
		return st, nil
	case Restricted:
	default:
		return st, ErrCantFix
	}
	elevate := el.Command
	if elevate == "" {
		if elevate = pickElevation(el.Graphical); elevate == "" {
			return st, ErrNoElevation
		}
	}
	tmp, err := os.CreateTemp("", ProfileName+"-*")
	if err != nil {
		return st, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(Profile(st.Bwrap)); err != nil {
		tmp.Close()
		return st, err
	}
	if err := tmp.Close(); err != nil {
		return st, err
	}
	cmd := exec.CommandContext(ctx, elevate, "/bin/sh", "-c", install, "sh", tmp.Name(), st.Profile, parser())
	var stderr bytes.Buffer
	cmd.Stdin, cmd.Stdout, cmd.Stderr = el.Stdin, el.Stdout, &stderr
	if el.Stderr != nil {
		cmd.Stderr = io.MultiWriter(el.Stderr, &stderr)
	}
	if err := run(cmd); err != nil {
		return st, fmt.Errorf("%s: %w: %s", elevate, err, strings.TrimSpace(stderr.String()))
	}
	after := Check(ctx)
	if after.State != Ready {
		return after, fmt.Errorf("the profile is in %s, but bubblewrap still fails: %s", after.Profile, after.Detail)
	}
	return after, nil
}

// pickElevation is sudo or else pkexec, whichever is installed (pkexec
// only for a graphical prompt), or "".
func pickElevation(graphical bool) string {
	order := []string{"sudo", "pkexec"}
	if graphical {
		order = []string{"pkexec"}
	}
	for _, c := range order {
		if p, err := lookPath(c); err == nil {
			return p
		}
	}
	return ""
}

// Commands are the commands Fix runs, for someone to run by hand: as root,
// write Profile(bwrap) to the profile's path and load it.
func Commands(st Status) string {
	return fmt.Sprintf("sudo tee %s <<'EOF'\n%sEOF\nsudo %s -r %s\n", st.Profile, Profile(st.Bwrap), parserOr("apparmor_parser"), st.Profile)
}

func parserOr(def string) string {
	if p := parser(); p != "" {
		return p
	}
	return def
}
