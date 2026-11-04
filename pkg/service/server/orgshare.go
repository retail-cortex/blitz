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

// maxShareScan bounds the files of a history read for secrets before it's
// shared (a variable for tests).
var maxShareScan = 50_000

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
		if filepath.Clean(sf.Dir) == dir {
			return nil, already
		}
	}

	// A repository of its own, with its work committed.
	var notes []string
	if top, err := gitText(ctx, dir, "rev-parse", "--show-toplevel"); err != nil {
		if _, err := gitText(ctx, dir, "init", "-q", "-b", "main"); err != nil {
			return nil, invalid(err)
		}
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

	// Nothing secret goes, in its files or its history.
	found, complete, err := secretscan.History(ctx, dir, "HEAD", maxShareScan)
	if err != nil {
		return nil, invalid(err)
	}
	if len(found) > 0 {
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
		return nil, apiError(connect.CodeFailedPrecondition, "SECRETS_FOUND", fmt.Errorf(
			"nothing was shared: the folder or its history holds what look like secrets: %s. Remove them from the files and the history, or mark a false alarm with %s on its line",
			b.String(), secretscan.AllowMarker))
	}
	if !complete {
		notes = append(notes, fmt.Sprintf("Its history is long: only its first %d files were checked for secrets.", maxShareScan))
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
	if err := h.s.git(ctx, dir, "push", "-q", sharedRemote, "HEAD:main"); err != nil {
		return nil, apiError(connect.CodeUnavailable, "PUSH_FAILED",
			fmt.Errorf("the workspace %s was made, but the folder couldn't be sent to it: %w", w.Handle, err))
	}
	trackShared(ctx, dir, sharedRemote)
	if err := addShared(c.URL(), w.Handle, sharedFolder{Dir: dir, Remote: sharedRemote}); err != nil {
		return nil, apiError(connect.CodeInternal, "INTERNAL", err)
	}
	h.s.ent.kickCatalog() // a folder to keep in step
	return ok(&pb.ShareWithOrganizationResponse{Workspace: h.s.orgWorkspaceMsg(w, folder, sharedFor(c.URL())), Notes: notes})
}
