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
	"github.com/retail-cortex/blitz/pkg/api"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
)

// ShellSandboxMode selects OS-level sandboxing for shell commands.
type ShellSandboxMode string

const (
	// SandboxAuto sandboxes shell commands when the platform supports it.
	SandboxAuto ShellSandboxMode = "auto"
	// SandboxRequired refuses to start unless shell commands can be sandboxed.
	SandboxRequired ShellSandboxMode = "required"
	// SandboxOff runs shell commands without an OS sandbox.
	SandboxOff ShellSandboxMode = "off"
)

// OSSandboxSpec describes what sandboxed commands may do. Reads are allowed
// everywhere except blocked paths; writes only under WritableDirs.
type OSSandboxSpec struct {
	Mode         ShellSandboxMode
	WritableDirs []string // canonical absolute directories
	ReadOnlyDirs []string // never writable, even when inside a writable dir
	Blocked      *PathMatcher
	AllowNetwork bool

	// shared, when set, is kept by the caller across sandboxes it builds
	// one after another (a skill script box, one per run): the platform
	// is probed once, and bubblewrap's blocked-path scan is reused.
	shared *sharedSandbox
}

// sharedSandbox is what sandboxes built for the same caller share.
type sharedSandbox struct {
	probed atomic.Bool
	scan   *blockedScan // built on first use, for the caller's blocked paths
	mu     sync.Mutex
}

// blockedScan returns the shared scan, made for spec's blocked paths the
// first time.
func (s *sharedSandbox) blockedScan(spec OSSandboxSpec) *blockedScan {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.scan == nil {
		s.scan = newBlockedScan(OSSandboxSpec{Blocked: spec.Blocked})
	}
	return s.scan
}

// OSSandbox wraps command argv so they run under the platform sandbox.
type OSSandbox struct {
	mode   ShellSandboxMode
	active bool
	reason string // why it is inactive
	wrapFn sandboxWrapper
	spec   OSSandboxSpec
}

// sandboxWrapper rewrites a command's argv to run it sandboxed. Getting
// ready may take a while (Linux scans for blocked paths): it stops with
// ctx's error when ctx ends first, and the command doesn't start.
type sandboxWrapper = func(ctx context.Context, argv []string) ([]string, error)

// platformSandbox returns a wrapper that sandboxes commands, or an error if
// this platform can't. Replaced in tests.
var platformSandbox = nativeSandbox

// prefixWrapper returns a wrapper that prepends a fixed argv prefix.
func prefixWrapper(prefix ...string) sandboxWrapper {
	return func(_ context.Context, argv []string) ([]string, error) {
		return append(append([]string(nil), prefix...), argv...), nil
	}
}

// NewOSSandbox sets up the shell sandbox. In "required" mode an unavailable
// sandbox is an error, so the CLI fails closed at startup rather than
// silently running commands unconfined.
func NewOSSandbox(spec OSSandboxSpec) (*OSSandbox, error) {
	switch spec.Mode {
	case "":
		spec.Mode = SandboxAuto
	case SandboxAuto, SandboxRequired, SandboxOff:
	default:
		return nil, fmt.Errorf("invalid sandbox.shell mode %q (use auto, required, or off)", spec.Mode)
	}
	s := &OSSandbox{mode: spec.Mode, spec: spec}
	if spec.Mode == SandboxOff {
		s.reason = "disabled by configuration"
		return s, nil
	}
	wrap, err := platformSandbox(spec)
	if err != nil {
		if spec.Mode == SandboxRequired {
			return nil, fmt.Errorf("sandbox.shell is \"required\" but %w: %v. %s", api.ErrSandboxUnavailable, err, sandboxHint)
		}
		s.reason = err.Error()
		return s, nil
	}
	s.active, s.wrapFn = true, wrap
	return s, nil
}

// Active reports whether shell commands run sandboxed.
func (s *OSSandbox) Active() bool { return s != nil && s.active }

// Status describes the sandbox state for users.
func (s *OSSandbox) Status() string {
	switch {
	case s == nil:
		return "off"
	case s.active:
		net := "network blocked"
		if s.spec.AllowNetwork {
			net = "network allowed"
		}
		return fmt.Sprintf("on (writes limited to %d dirs, %s)", len(s.spec.WritableDirs), net)
	default:
		return "off: " + s.reason
	}
}

// wrapCtx is argv sandboxed, or ctx's error if ctx ends while the sandbox
// gets ready for it.
func (s *OSSandbox) wrapCtx(ctx context.Context, argv []string) ([]string, error) {
	if !s.Active() {
		return argv, nil
	}
	return s.wrapFn(ctx, argv)
}

// DefaultShellWritableDirs returns scratch locations commands commonly need:
// the temp dirs and the user cache dir (e.g. the Go build cache).
func DefaultShellWritableDirs() []string {
	var dirs []string
	add := func(d string) {
		if d == "" {
			return
		}
		if real, err := canonicalDir(d); err == nil {
			dirs = append(dirs, real)
		}
	}
	tmp := os.TempDir()
	add(tmp)
	// macOS per-user temp lives in /var/folders/xx/yyy/T; its siblings (C, 0)
	// hold per-user caches that many tools write to.
	if strings.HasPrefix(tmp, "/var/folders/") || strings.HasPrefix(tmp, "/private/var/folders/") {
		add(filepath.Dir(filepath.Clean(tmp)))
	}
	add("/tmp")
	add("/var/tmp")
	if cache, err := os.UserCacheDir(); err == nil {
		add(cache)
	}
	return dirs
}

// seatbeltProfile renders a macOS sandbox profile for spec. Rule order
// matters: later rules override earlier ones, so blocked paths come last.
func seatbeltProfile(spec OSSandboxSpec) string {
	var sb strings.Builder
	sb.WriteString("(version 1)\n(allow default)\n(deny file-write*)\n(allow file-write*\n")
	for _, d := range spec.WritableDirs {
		fmt.Fprintf(&sb, "  (subpath %s)\n", sbplString(d))
	}
	sb.WriteString("  (literal \"/dev/null\") (literal \"/dev/zero\") (literal \"/dev/tty\") (literal \"/dev/dtracehelper\")\n")
	sb.WriteString("  (regex #\"^/dev/ttys[0-9]+$\") (regex #\"^/dev/fd/\"))\n")
	if len(spec.ReadOnlyDirs) > 0 {
		sb.WriteString("(deny file-write*\n")
		for _, d := range spec.ReadOnlyDirs {
			fmt.Fprintf(&sb, "  (subpath %s)\n", sbplString(d))
		}
		sb.WriteString(")\n")
	}
	denyPrivateDirs(&sb, privateDirs(), append(slices.Clone(spec.WritableDirs), spec.ReadOnlyDirs...))
	if regexes := spec.Blocked.sbplRegexes(); len(regexes) > 0 {
		sb.WriteString("(deny file-read* file-write*\n")
		for _, re := range regexes {
			fmt.Fprintf(&sb, "  (regex #\"%s\")\n", strings.ReplaceAll(re, `"`, `\"`))
		}
		sb.WriteString(")\n")
	}
	if !spec.AllowNetwork {
		sb.WriteString("(deny network*)\n(allow network* (remote unix-socket))\n")
	}
	denyServiceSockets(&sb)
	return sb.String()
}

// denyServiceSockets keeps commands from connecting to the service's
// sockets and anything in Blitz's run folder, or writing (moving a socket
// away) there, whatever the network setting: last, so it wins over the
// Unix sockets allowed above. The copies of credentials in the run folder
// stay readable.
func denyServiceSockets(sb *strings.Builder) {
	dirs, socks := serviceSockets()
	if len(dirs)+len(socks) == 0 {
		return
	}
	sb.WriteString("(deny network-outbound file-write*\n")
	for _, d := range dirs {
		fmt.Fprintf(sb, "  (subpath %s)\n", sbplString(d))
	}
	for _, s := range socks {
		fmt.Fprintf(sb, "  (literal %s)\n", sbplString(s))
	}
	sb.WriteString(")\n")
}

// privateDirs are the folders macOS guards with a prompt ("Blitz would like
// to access files in your Music folder"): the user's personal folders,
// iCloud Drive, the apps' own data, and other volumes. A command that
// sweeps the disk (find /, a search for a tool) would make macOS ask on
// Blitz's behalf, once per folder and again after every update (each build
// is a new app to macOS). Commands can't read inside them, so they skip
// them instead; a workspace or allowed folder inside one stays readable.
var privateDirs = func() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return []string{"/Volumes"}
	}
	dirs := []string{"/Volumes"}
	for _, d := range []string{
		"Desktop", "Documents", "Downloads", "Movies", "Music", "Pictures",
		"Library/Mobile Documents", "Library/Mail", "Library/Messages", "Library/Calendars",
		"Library/Application Support/AddressBook", "Library/Safari",
	} {
		dirs = append(dirs, filepath.Join(home, d))
	}
	return dirs
}

// denyPrivateDirs denies reading inside each private dir (and listing it: its
// own metadata stays readable, so ls -la ~ still works), then allows again
// the roots inside one (later rules win).
func denyPrivateDirs(sb *strings.Builder, private, roots []string) {
	if len(private) == 0 {
		return
	}
	sb.WriteString("(deny file-read*\n")
	for _, d := range private {
		fmt.Fprintf(sb, "  (regex #\"^%s/\")\n", strings.ReplaceAll(regexp.QuoteMeta(d), `"`, `\"`))
	}
	sb.WriteString(")\n(deny file-read-data\n")
	for _, d := range private {
		fmt.Fprintf(sb, "  (literal %s)\n", sbplString(d))
	}
	sb.WriteString(")\n")
	var inside []string
	for _, r := range roots {
		for _, d := range private {
			if r == d || strings.HasPrefix(r, d+string(filepath.Separator)) {
				inside = append(inside, r)
				break
			}
		}
	}
	if len(inside) > 0 {
		sb.WriteString("(allow file-read*\n")
		for _, r := range inside {
			fmt.Fprintf(sb, "  (subpath %s)\n", sbplString(r))
		}
		sb.WriteString(")\n")
	}
}

func sbplString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}
