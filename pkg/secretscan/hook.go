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

package secretscan

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// HooksDir is where the hook goes, relative to the repository's top:
// versioned, so everyone who clones it gets it (core.hooksPath).
const HooksDir = ".githooks"

// hookHeader starts every hook Blitz writes, so Install knows its own.
const hookHeader = "#!/bin/sh\n# Blitz: stops a commit that would add an API key or another secret.\n"

// HookScript is .githooks/pre-commit: Blitz's scan when blitz is on PATH,
// else a portable search of the lines being added for the same key
// formats.
const HookScript = `#!/bin/sh
# Blitz: stops a commit that would add an API key or another secret.
# A false alarm: add "blitz:allow-secret" to the line, or commit with
# git commit --no-verify. (git config core.hooksPath .githooks turns it on
# in a new clone.)
# Only a blitz that has the scan: an older one on PATH would refuse it.
if command -v blitz >/dev/null 2>&1 && blitz security --help 2>/dev/null | grep -q 'scan-secrets'; then
	exec blitz security scan-secrets --staged
fi
added=$(git diff --cached -U0 --no-color --diff-filter=ACMR | grep '^+' | grep -v '^+++' | grep -v 'blitz:allow-secret')
pattern='-----BEGIN [A-Z ]*PRIVATE KEY-----|(AKIA|ASIA)[0-9A-Z]{16}|sk-(ant-|proj-)?[A-Za-z0-9_-]{20,}|AIza[0-9A-Za-z_-]{35}|(ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{60,}|glpat-[A-Za-z0-9_-]{20,}|xox[abprs]-[A-Za-z0-9-]{10,}|(sk|rk)_live_[A-Za-z0-9]{20,}|hf_[A-Za-z0-9]{30,}'
if printf '%s\n' "$added" | grep -Eq -e "$pattern"; then
	echo "Blitz: this commit adds what looks like an API key or a private key:" >&2
	printf '%s\n' "$added" | grep -Eo -e "$pattern" | cut -c1-4 | sed 's/$/••••••••/' >&2
	echo "Take it out (keep it in the keychain or an ignored .env), mark the line blitz:allow-secret, or commit with --no-verify." >&2
	exit 1
fi
exit 0
`

// Install results.
const (
	// Installed: the hook was written and core.hooksPath set.
	Installed = "installed"
	// Present: the repository already has it.
	Present = "present"
	// OtherHooks: the repository uses other hooks (core.hooksPath set
	// elsewhere, husky, the pre-commit framework, a .git/hooks/pre-commit);
	// nothing was changed. Detail says which.
	OtherHooks = "other_hooks"
)

// ErrNotARepository is Install outside a git repository.
var ErrNotARepository = errors.New("not a git repository")

// Result is what Install did.
type Result struct {
	Status string
	// Path is the hook, absolute.
	Path   string
	Detail string
}

func gitIn(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

// Install puts the hook in the repository dir is in: .githooks/pre-commit
// at its top and core.hooksPath in its own config. A repository with other
// hooks is left as it is (OtherHooks).
func Install(ctx context.Context, dir string) (Result, error) {
	top, err := gitIn(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return Result{}, fmt.Errorf("%w: %s", ErrNotARepository, dir)
	}
	hook := filepath.Join(top, HooksDir, "pre-commit")
	current, _ := gitIn(ctx, top, "config", "--local", "--get", "core.hooksPath")
	data, readErr := os.ReadFile(hook)
	ours := readErr == nil && strings.HasPrefix(string(data), hookHeader)
	if ours && string(data) == HookScript && current == HooksDir {
		return Result{Status: Present, Path: hook}, nil
	}
	if current != "" && current != HooksDir {
		return Result{Status: OtherHooks, Path: hook, Detail: "core.hooksPath is " + current}, nil
	}
	for _, other := range []string{".husky", ".pre-commit-config.yaml", ".lefthook.yml", "lefthook.yml"} {
		if _, err := os.Stat(filepath.Join(top, other)); err == nil {
			return Result{Status: OtherHooks, Path: hook, Detail: other}, nil
		}
	}
	if gitDir, err := gitIn(ctx, top, "rev-parse", "--absolute-git-dir"); err == nil && !ours {
		if info, err := os.Stat(filepath.Join(gitDir, "hooks", "pre-commit")); err == nil && info.Mode().IsRegular() {
			return Result{Status: OtherHooks, Path: hook, Detail: filepath.Join(gitDir, "hooks", "pre-commit")}, nil
		}
	}
	// A hook of the repository's own is left alone; one Blitz wrote
	// before is brought up to date.
	if readErr == nil && !ours {
		return Result{Status: OtherHooks, Path: hook, Detail: filepath.Join(HooksDir, "pre-commit")}, nil
	}
	if err := os.MkdirAll(filepath.Dir(hook), 0o755); err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(hook, []byte(HookScript), 0o755); err != nil {
		return Result{}, err
	}
	if err := os.Chmod(hook, 0o755); err != nil { // past the umask
		return Result{}, err
	}
	if _, err := gitIn(ctx, top, "config", "--local", "core.hooksPath", HooksDir); err != nil {
		return Result{}, err
	}
	return Result{Status: Installed, Path: hook}, nil
}

// Staged is what the commit being made adds: each added, copied, changed
// or renamed file's content as staged, by its path from the repository's
// top.
func Staged(ctx context.Context, dir string) (map[string][]byte, error) {
	top, err := gitIn(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrNotARepository, dir)
	}
	names, err := gitIn(ctx, top, "diff", "--cached", "--name-only", "-z", "--diff-filter=ACMR")
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for name := range strings.SplitSeq(names, "\x00") {
		if name == "" {
			continue
		}
		cmd := exec.CommandContext(ctx, "git", "show", ":"+name)
		cmd.Dir = top
		data, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("git show :%s: %w", name, err)
		}
		out[name] = data
	}
	return out, nil
}
