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

// Package runmerge adds a job run's changes, on the run's own branch, to
// its workspace's main (blitz-enterprise spec_009_jobs JB-10): the runner
// does it as the run ends, as the job says, and Blitz when someone adds a
// run's changes later.
package runmerge

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Git runs git with args in the clone the merge is made in.
type Git func(ctx context.Context, args ...string) ([]byte, error)

// ErrConflicts is a run's changes and main's changing the same lines:
// they stay on the run's branch.
var ErrConflicts = errors.New("its changes and main's change the same lines")

// tries is how often a merge is tried again when main moves before it's
// pushed.
const tries = 3

// IntoMain merges remote's branch into remote's main and pushes main, in
// the clone git runs in (which it leaves on main), as who ("Blitz job
// digest"); again, up to three times, when someone pushes main meanwhile.
// A push refused for another reason (sign-in, a hook) isn't tried again,
// and its error says what git said. A branch main has already is no
// change.
func IntoMain(ctx context.Context, git Git, remote, branch, who string) error {
	mainRef, runRef := remote+"/main", remote+"/"+branch
	var err error
	for range tries {
		if _, err = git(ctx, "fetch", "--quiet", remote,
			"+refs/heads/main:refs/remotes/"+mainRef, "+refs/heads/"+branch+":refs/remotes/"+runRef); err != nil {
			return fmt.Errorf("fetching main and %s: %w", branch, err)
		}
		if _, err = git(ctx, "checkout", "--quiet", "--force", "-B", "main", mainRef); err != nil {
			return err
		}
		if _, err = git(ctx, "-c", "user.name="+who, "-c", "user.email=jobs@blitz.invalid",
			"merge", "--quiet", "--no-edit", "-m", "Add "+branch+" to main", runRef); err != nil {
			out, _ := git(ctx, "diff", "--name-only", "--diff-filter=U")
			_, _ = git(ctx, "merge", "--abort")
			if strings.TrimSpace(string(out)) != "" {
				return ErrConflicts
			}
			return err
		}
		out, perr := git(ctx, "push", "--quiet", remote, "main")
		if perr == nil {
			return nil
		}
		// git's words are in its output or its error, as the Git given
		// keeps them.
		if text := string(out) + "\n" + perr.Error(); !moved(text) { // refused (sign-in, a hook, protection): trying again won't help
			return fmt.Errorf("pushing main: %w%s", perr, said(out, perr))
		}
		err = fmt.Errorf("main kept moving (tried %d times): %w", tries, perr)
	}
	return fmt.Errorf("pushing main: %w", err)
}

// moved reports whether git push's words say main moved on the remote
// (the push isn't a fast-forward any more), the one refusal another try
// can get past.
func moved(text string) bool {
	return strings.Contains(text, "[rejected]") && (strings.Contains(text, "fetch first") || strings.Contains(text, "non-fast-forward"))
}

// said is git's refusal from its output (the " ! [remote rejected] …"
// line), after ": ", unless err has it already ("" when there's none).
func said(out []byte, err error) string {
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); strings.HasPrefix(l, "! ") && !strings.Contains(err.Error(), l) {
			return ": " + l
		}
	}
	return ""
}
