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
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
)

// History finds the secrets in every file of the history ref has in the
// repository at dir (each version of each file once, those under 1 MB),
// for a repository about to be shared: its past commits go with it. At
// most maxFiles files are read (folders don't count); complete says
// whether that was all of them. A version seen at several paths is read
// as a settings file when one of them is one (.env, a .json…), where a
// long random value assigned to a key-like name is a secret.
func History(ctx context.Context, dir, ref string, maxFiles int) (found []Finding, complete bool, err error) {
	list, err := gitIn(ctx, dir, "rev-list", "--objects", "--filter=blob:limit=1m", "--filter-provided-objects", ref)
	if err != nil {
		// An older Git without --filter-provided-objects.
		if list, err = gitIn(ctx, dir, "rev-list", "--objects", ref); err != nil {
			return nil, false, err
		}
	}
	// Each object once, with the path it's read as.
	paths := map[string]string{}
	var order []string
	for _, line := range strings.Split(list, "\n") {
		sha, path, ok := strings.Cut(line, " ")
		if !ok || path == "" {
			continue // a commit, or the root tree
		}
		first, seen := paths[sha]
		if !seen {
			order = append(order, sha)
		}
		if !seen || !configFile(first) && configFile(path) {
			paths[sha] = path
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "cat-file", "--batch")
	cmd.Dir = dir
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, false, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, false, err
	}
	if err := cmd.Start(); err != nil {
		return nil, false, err
	}
	go func() {
		defer in.Close()
		for _, sha := range order {
			if _, err := fmt.Fprintln(in, sha); err != nil {
				return
			}
		}
	}()
	complete = true
	files := 0
	r := bufio.NewReader(out)
	for {
		head, err := r.ReadString('\n')
		if err == io.EOF {
			break
		}
		if err != nil {
			_ = cmd.Wait()
			return nil, false, err
		}
		f := strings.Fields(head) // <sha> <type> <size>, or <sha> missing
		if len(f) < 3 {
			continue
		}
		if f[1] == "blob" && files == maxFiles {
			// One more file than it reads: the rest isn't read at all.
			complete = false
			cancel()
			_ = cmd.Wait()
			return found, complete, nil
		}
		size, _ := strconv.Atoi(f[2])
		body := make([]byte, size+1) // its content and a newline
		if _, err := io.ReadFull(r, body); err != nil {
			_ = cmd.Wait()
			return nil, false, err
		}
		if f[1] == "blob" {
			files++
			found = append(found, Scan(paths[f[0]], bytes.TrimSuffix(body, []byte("\n")))...)
		}
	}
	if err := cmd.Wait(); err != nil {
		return nil, false, err
	}
	return found, complete, nil
}
