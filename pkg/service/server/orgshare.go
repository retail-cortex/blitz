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

package server

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"connectrpc.com/connect"

	"github.com/retail-cortex/blitz/pkg/enterprise"
	"github.com/retail-cortex/blitz/pkg/secretscan"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// Sharing a folder with the organization where it is (spec_053 EN-66).

// maxShareScan bounds the files read for secrets before a folder is shared,
// its changes and its history together: a folder with more isn't shared
// (a variable for tests).
var maxShareScan = 50_000

// maxShareFile is the size past which a changed file isn't read for
// secrets, as History leaves its larger files.
const maxShareFile = 1 << 20

// largeShareFile is the size past which a file is mentioned before it's
// shared (the service's own limit is the workspace's; a variable for
// tests).
var largeShareFile int64 = 50 << 20

// gitText runs git in dir, its output trimmed.
func gitText(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func (h configService) ShareWithOrganization(ctx context.Context, r req[pb.ShareWithOrganizationRequest]) (*connect.Response[pb.ShareWithOrganizationResponse], error) {
	c, cfg, err := h.s.orgClient()
	if err != nil {
		return nil, err
	}
	if !c.Status().SignedIn {
		return nil, enterpriseError(enterprise.ErrNotSignedIn)
	}
	dir, err := filepath.Abs(r.Msg.Workspace)
	if err != nil {
		return nil, invalid(err)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return nil, invalid(fmt.Errorf("%s isn't a folder", r.Msg.Workspace))
	}
	folder := h.s.orgFolder(c, cfg)
	already := apiError(connect.CodeFailedPrecondition, "ALREADY_SHARED", fmt.Errorf("%s is one of the organization's workspaces already", dir))
	if rel, err := filepath.Rel(folder, dir); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, already
	}
	for _, sf := range sharedFor(c.URL()) {
		if samePath(sf.Dir, dir) {
			return nil, already
		}
	}

	// A repository of its own, with its work committed.
	var notes []string
	if top, err := gitText(ctx, dir, "rev-parse", "--show-toplevel"); err != nil {
		if _, err := gitText(ctx, dir, "init", "-q", "-b", "main"); err != nil {
			return nil, invalid(err)
		}
		// A refusal before its first commit leaves the folder as it was.
		defer func() {
			if _, err := gitText(context.WithoutCancel(ctx), dir, "rev-parse", "--verify", "-q", "HEAD"); err != nil {
				_ = os.RemoveAll(filepath.Join(dir, ".git"))
			}
		}()
	} else if real, _ := filepath.EvalSymlinks(dir); filepath.Clean(top) != dir && filepath.Clean(top) != real {
		return nil, apiError(connect.CodeFailedPrecondition, "INSIDE_REPOSITORY",
			fmt.Errorf("%s is inside the repository %s: share that folder instead", dir, top))
	}
	who := asPerson(c.Status().Person)
	changes, err := gitText(ctx, dir, "status", "--porcelain")
	if err != nil {
		return nil, invalid(err)
	}
	_, noHead := gitText(ctx, dir, "rev-parse", "--verify", "-q", "HEAD")

	// Nothing secret goes, in its files or its history: its changes are
	// read before they're committed, so a refusal leaves no commit behind.
	found, read, err := changedSecrets(ctx, dir, maxShareScan)
	if err != nil {
		return nil, invalid(err)
	}
	complete := read <= maxShareScan
	if complete && noHead == nil {
		var more []secretscan.Finding
		more, complete, err = secretscan.History(ctx, dir, "HEAD", maxShareScan-read)
		if err != nil {
			return nil, invalid(err)
		}
		found = append(found, more...)
	}
	if len(found) > 0 {
		return nil, apiError(connect.CodeFailedPrecondition, "SECRETS_FOUND", fmt.Errorf(
			"nothing was shared: the folder or its history holds what look like secrets: %s. Remove them from the files and the history, or mark a false alarm with %s on its line",
			findings(found), secretscan.AllowMarker))
	}
	if !complete {
		// What wasn't read could hold a key: nothing goes unread (EN-66).
		return nil, apiError(connect.CodeFailedPrecondition, "SECRETS_UNCHECKED", fmt.Errorf(
			"nothing was shared: the folder and its history hold more than %d files, too many to check for secrets. Share a copy of the folder without its history (a new repository)", maxShareScan),
			"max_files", strconv.Itoa(maxShareScan))
	}

	// Its work committed.
	if changes != "" || noHead != nil {
		if _, err := gitText(ctx, dir, "add", "-A"); err != nil {
			return nil, invalid(err)
		}
		msg := "Shared with " + filepath.Base(folder)
		if _, err := gitText(ctx, dir, append(who, "commit", "-q", "--allow-empty", "-m", msg)...); err != nil {
			return nil, invalid(err)
		}
		if changes != "" {
			notes = append(notes, "Your changes not yet committed were committed first, as “"+msg+"”.")
		}
	}
	if tree, err := gitText(ctx, dir, "ls-tree", "-r", "-l", "HEAD"); err == nil {
		for _, line := range strings.Split(tree, "\n") {
			f := strings.Fields(line) // <mode> blob <sha> <size>\t<path>
			if len(f) < 5 {
				continue
			}
			if size, _ := strconv.ParseInt(f[3], 10, 64); size > largeShareFile {
				notes = append(notes, fmt.Sprintf("%s is %d MB: large files make every copy slow to sync.", strings.Join(f[4:], " "), size>>20))
			}
		}
	}

	// The workspace, and the folder pushed to it.
	name := cmp.Or(r.Msg.Name, filepath.Base(dir))
	w, err := c.CreateWorkspace(ctx, name, r.Msg.Handle, r.Msg.Personal)
	if err != nil {
		return nil, enterpriseError(err)
	}
	if _, err := gitText(ctx, dir, "remote", "get-url", sharedRemote); err == nil {
		_, err = gitText(ctx, dir, "remote", "set-url", sharedRemote, w.CloneURL)
		if err != nil {
			return nil, invalid(err)
		}
	} else if _, err := gitText(ctx, dir, "remote", "add", sharedRemote, w.CloneURL); err != nil {
		return nil, invalid(err)
	}
	if err := h.s.setHelper(ctx, dir, c.URL()); err != nil {
		return nil, invalid(err)
	}
	// Recorded before it's sent: a push that fails leaves the folder
	// shared, and its sync sends it (sharing again would make a second
	// workspace, or find the handle taken).
	if err := addShared(c.URL(), w.Handle, sharedFolder{Dir: dir, Remote: sharedRemote}); err != nil {
		return nil, apiError(connect.CodeInternal, "INTERNAL", err)
	}
	if err := h.s.git(ctx, dir, "push", "-q", sharedRemote, "HEAD:main"); err != nil {
		h.s.ent.kickCatalog()
		return nil, apiError(connect.CodeUnavailable, "PUSH_FAILED",
			fmt.Errorf("the workspace %s was made and the folder is shared with it, but it couldn't be sent yet: it's sent as it syncs (Save now in Changes tries at once): %w", w.Handle, err),
			"handle", w.Handle)
	}
	trackShared(ctx, dir, sharedRemote)
	h.s.ent.kickCatalog() // a folder to keep in step
	return ok(&pb.ShareWithOrganizationResponse{Workspace: h.s.orgWorkspaceMsg(w, folder, sharedFor(c.URL())), Notes: notes})
}

// findings names what a secret scan found, each by file and line, the
// first ten.
func findings(found []secretscan.Finding) string {
	var b strings.Builder
	for i, f := range found {
		if i == 10 {
			fmt.Fprintf(&b, "; and %d more", len(found)-10)
			break
		}
		if i > 0 {
			b.WriteString("; ")
		}
		fmt.Fprintf(&b, "%s:%d (%s %s)", f.Path, f.Line, f.Kind, f.Masked)
	}
	return b.String()
}

// changedSecrets finds the secrets in what committing everything in the
// repository at dir would add (its untracked files git doesn't ignore, and
// its files that differ from HEAD), reading them from the folder, at most
// maxFiles of them; read is how many there were.
func changedSecrets(ctx context.Context, dir string, maxFiles int) (found []secretscan.Finding, read int, err error) {
	untracked, err := gitText(ctx, dir, "ls-files", "-z", "-o", "--exclude-standard")
	if err != nil {
		return nil, 0, err
	}
	changed, err := gitText(ctx, dir, "diff", "--name-only", "-z", "HEAD")
	if err != nil {
		// No HEAD yet: every file in the index is new.
		if changed, err = gitText(ctx, dir, "ls-files", "-z", "-c"); err != nil {
			return nil, 0, err
		}
	}
	seen := map[string]bool{}
	for name := range strings.SplitSeq(untracked+"\x00"+changed, "\x00") {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		fi, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil || !fi.Mode().IsRegular() || fi.Size() > maxShareFile {
			continue // removed, a link or a folder, or too large to read (as History)
		}
		if read++; read > maxFiles {
			return found, read, nil
		}
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			return nil, 0, err
		}
		found = append(found, secretscan.Scan(name, data)...)
	}
	return found, read, nil
}
