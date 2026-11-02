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
// most maxFiles are read; complete says whether that was all of them.
func History(ctx context.Context, dir, ref string, maxFiles int) (found []Finding, complete bool, err error) {
	list, err := gitIn(ctx, dir, "rev-list", "--objects", "--filter=blob:limit=1m", "--filter-provided-objects", ref)
	if err != nil {
		// An older Git without --filter-provided-objects.
		if list, err = gitIn(ctx, dir, "rev-list", "--objects", ref); err != nil {
			return nil, false, err
		}
	}
	// Each object once, with the first path it's seen at.
	paths := map[string]string{}
	var order []string
	for _, line := range strings.Split(list, "\n") {
		sha, path, ok := strings.Cut(line, " ")
		if !ok || path == "" {
			continue // a commit, or the root tree
		}
		if _, seen := paths[sha]; !seen {
			paths[sha] = path
			order = append(order, sha)
		}
	}
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
	complete = len(order) <= maxFiles
	order = order[:min(len(order), maxFiles)]
	go func() {
		defer in.Close()
		for _, sha := range order {
			if _, err := fmt.Fprintln(in, sha); err != nil {
				return
			}
		}
	}()
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
		size, _ := strconv.Atoi(f[2])
		body := make([]byte, size+1) // its content and a newline
		if _, err := io.ReadFull(r, body); err != nil {
			_ = cmd.Wait()
			return nil, false, err
		}
		if f[1] == "blob" {
			found = append(found, Scan(paths[f[0]], bytes.TrimSuffix(body, []byte("\n")))...)
		}
	}
	if err := cmd.Wait(); err != nil {
		return nil, false, err
	}
	return found, complete, nil
}
