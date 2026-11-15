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

package enterprise

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/retail-cortex/blitz/pkg/agentpkg"
)

// PersonalItem is one of the person's own assets as the organization's
// service keeps it (spec_053 EN-93; the service's spec_014 LB-50): its
// files are paths under ~/.blitz ("skills/tone/SKILL.md").
type PersonalItem struct {
	Kind      string            `json:"kind"`
	Name      string            `json:"name"`
	Revision  int               `json:"revision"`
	SHA256    string            `json:"sha256"`
	ChangedAt time.Time         `json:"changed_at"`
	Files     map[string]string `json:"files,omitempty"`
}

// ErrChangedElsewhere is a change to a personal asset another computer
// changed first (the service's 409 conflict).
var ErrChangedElsewhere = errors.New("changed on another computer first")

// PersonalItems are the person's own assets the service keeps (no files)
// and which kinds it keeps, by kind.
func (c *Client) PersonalItems(ctx context.Context) ([]PersonalItem, map[string]bool, error) {
	var out struct {
		Items   []PersonalItem  `json:"items"`
		Allowed map[string]bool `json:"allowed"`
	}
	err := c.signedCall(ctx, http.MethodGet, "/api/v1/me/library", nil, &out)
	return out.Items, out.Allowed, err
}

// PersonalItem is one of them, with its files.
func (c *Client) PersonalItem(ctx context.Context, kind, name string) (PersonalItem, error) {
	var out PersonalItem
	err := c.signedCall(ctx, http.MethodGet, "/api/v1/me/library/"+url.PathEscape(kind)+"/"+url.PathEscape(name), nil, &out)
	return out, err
}

// PutPersonal keeps files as the person's asset, a change from revision
// base (0: a new one); ErrChangedElsewhere when another came first.
func (c *Client) PutPersonal(ctx context.Context, kind, name string, files map[string]string, base int) (PersonalItem, error) {
	var out PersonalItem
	err := c.signedCall(ctx, http.MethodPut, "/api/v1/me/library/"+url.PathEscape(kind)+"/"+url.PathEscape(name), map[string]any{"files": files, "base": base}, &out)
	return out, conflicted(err)
}

// DeletePersonal removes the person's asset, a change from revision base.
func (c *Client) DeletePersonal(ctx context.Context, kind, name string, base int) error {
	return conflicted(c.signedCall(ctx, http.MethodDelete, "/api/v1/me/library/"+url.PathEscape(kind)+"/"+url.PathEscape(name)+"?base="+strconv.Itoa(base), nil, nil))
}

func conflicted(err error) error {
	if ref, ok := errors.AsType[*Refusal](err); ok && ref.Status == http.StatusConflict && ref.Code == "conflict" {
		return fmt.Errorf("%w: %s", ErrChangedElsewhere, ref.Message)
	}
	return err
}

// RunReport is one of the person's workflow or eval runs, as the service
// keeps it (EN-94; the service's LB-54).
type RunReport struct {
	Kind      string     `json:"kind"` // workflow or eval
	Name      string     `json:"name"`
	Status    string     `json:"status"`
	Workspace string     `json:"workspace,omitempty"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	CostUSD   float64    `json:"cost_usd"`
	Summary   string     `json:"summary,omitempty"`
}

// ReportRun tells the service of a run.
func (c *Client) ReportRun(ctx context.Context, r RunReport) error {
	return c.signedCall(ctx, http.MethodPost, "/api/v1/me/runs", r, nil)
}

// Verdict is one of an evaluator's checks' on a submission.
type Verdict struct {
	Check    string   `json:"check"`
	Score    float64  `json:"score"`
	Passed   bool     `json:"passed"`
	Reason   string   `json:"reason"`
	Findings []string `json:"findings,omitempty"`
}

// Submission is one of the person's assets submitted to share with the
// organization (EN-95; the service's LB-60): judging, rejected, held for
// an admin, accepted (then its published version) or error.
type Submission struct {
	ID        int64      `json:"id"`
	Kind      string     `json:"kind"`
	Name      string     `json:"name"`
	Note      string     `json:"note"`
	State     string     `json:"state"`
	Verdicts  []Verdict  `json:"verdicts"`
	Reason    string     `json:"reason"`
	Published string     `json:"published_version,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	DecidedAt *time.Time `json:"decided_at,omitempty"`
}

// Share submits files as the person's kind/name to share with the
// organization, judged by its evaluator: a Refusal (contribute_off,
// no_evaluator, name_taken, submission_open, invalid) when it isn't taken.
func (c *Client) Share(ctx context.Context, kind, name string, files map[string]string, note string) (Submission, error) {
	var out Submission
	err := c.signedCall(ctx, http.MethodPost, "/api/v1/me/library/"+url.PathEscape(kind)+"/"+url.PathEscape(name)+"/share", map[string]any{"files": files, "note": note}, &out)
	return out, err
}

// Submissions are the person's submissions, the newest first.
func (c *Client) Submissions(ctx context.Context) ([]Submission, error) {
	var out struct {
		Submissions []Submission `json:"submissions"`
	}
	err := c.signedCall(ctx, http.MethodGet, "/api/v1/me/submissions", nil, &out)
	return out.Submissions, err
}

// PersonalKinds are the kinds of the person's own assets that sync, each
// with its folder under ~/.blitz.
var PersonalKinds = map[string]string{"agent": "agents", "skill": "skills", "workflow": "workflows", "rule": "rules", "guardrail": "guardrails"}

// personalName is a name the service takes.
var personalName = regexp.MustCompile(`^[a-z0-9]+([-_][a-z0-9]+)*$`)

// The most of an asset the service keeps (its LB-04).
const (
	maxPersonalFile  = 1 << 20
	maxPersonalTotal = 5 << 20
	maxPersonalFiles = 200
)

// PersonalFiles are the person's own assets under root (~/.blitz), by
// "kind/name", each its files by path under root; skipped are those that
// can't sync, and why.
func PersonalFiles(root string) (map[string]map[string]string, map[string]string) {
	out, skipped := map[string]map[string]string{}, map[string]string{}
	for kind, dir := range PersonalKinds {
		entries, err := os.ReadDir(filepath.Join(root, dir))
		if err != nil {
			continue
		}
		for _, e := range entries {
			name, ok := personalItemName(kind, e)
			if !ok {
				continue
			}
			key := kind + "/" + name
			if !personalName.MatchString(name) || len(name) > 64 {
				skipped[key] = "its name isn't lowercase letters, digits and single hyphens or underscores"
				continue
			}
			files, why := readPersonal(root, kind, dir, name)
			if why != "" {
				skipped[key] = why
				continue
			}
			out[key] = files
		}
	}
	return out, skipped
}

// personalItemName is the asset e is in a kind's folder, if it is one.
func personalItemName(kind string, e fs.DirEntry) (string, bool) {
	n := e.Name()
	if strings.HasPrefix(n, ".") {
		return "", false
	}
	switch kind {
	case "skill":
		return n, e.IsDir()
	case "workflow":
		name, ok := strings.CutSuffix(n, ".flow.yaml")
		return name, ok && !e.IsDir()
	}
	name, ok := strings.CutSuffix(n, ".md")
	return name, ok && !e.IsDir()
}

// readPersonal is an asset's files, or why it can't sync.
func readPersonal(root, kind, dir, name string) (map[string]string, string) {
	files := map[string]string{}
	add := func(rel string) string {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		switch {
		case err != nil:
			return err.Error()
		case len(b) > maxPersonalFile:
			return rel + " is over 1 MB"
		case !utf8.Valid(b):
			return rel + " isn't text"
		}
		files[rel] = string(b)
		return ""
	}
	switch kind {
	case "skill":
		base := filepath.Join(root, dir, name)
		var why string
		err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if strings.HasPrefix(d.Name(), ".") && p != base {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() || !d.Type().IsRegular() {
				return nil
			}
			rel, _ := filepath.Rel(root, p)
			if why = add(filepath.ToSlash(rel)); why != "" {
				return fs.SkipAll
			}
			return nil
		})
		if err != nil {
			return nil, err.Error()
		}
		if why != "" {
			return nil, why
		}
	case "workflow":
		if why := add(dir + "/" + name + ".flow.yaml"); why != "" {
			return nil, why
		}
	default:
		if why := add(dir + "/" + name + ".md"); why != "" {
			return nil, why
		}
	}
	total := 0
	for _, t := range files {
		total += len(t)
	}
	if total > maxPersonalTotal || len(files) > maxPersonalFiles {
		return nil, "it's over 5 MB or 200 files"
	}
	return files, ""
}

// SyncReport is what a sync of the person's own assets did (EN-93), by
// "kind/name".
type SyncReport struct {
	Sent, Received, Removed []string
	// Conflicts were changed on this computer and another: this one's
	// kept and sent, the other's saved under ~/.blitz/conflicts.
	Conflicts []string
	// Skipped can't sync, and why: a kind the policy doesn't keep, a
	// name or file the service doesn't take.
	Skipped map[string]string
}

// syncState is what the last sync left, by "kind/name".
type syncState struct {
	Items map[string]syncedItem `json:"items"`
}

type syncedItem struct {
	Revision int    `json:"revision"`
	SHA256   string `json:"sha256"`
}

// ConflictsDir is where a sync keeps another computer's version of an
// asset changed on both, under ~/.blitz.
const ConflictsDir = "conflicts"

// SyncPersonal syncs the person's own assets under root (~/.blitz) with
// the organization's service, both ways (EN-93), remembering what it
// synced in state (a file): what changed here is sent, what changed on
// another computer is fetched, what was removed on one side is removed on
// the other; changed on both, this computer's wins and the other's is
// kept under root/conflicts. Kinds the policy doesn't keep are left alone.
func SyncPersonal(ctx context.Context, c *Client, root, state string) (SyncReport, error) {
	rep := SyncReport{Skipped: map[string]string{}}
	remote, allowed, err := c.PersonalItems(ctx)
	if err != nil {
		return rep, err
	}
	local, skipped := PersonalFiles(root)
	var st syncState
	if b, err := os.ReadFile(state); err == nil {
		_ = json.Unmarshal(b, &st)
	}
	if st.Items == nil {
		st.Items = map[string]syncedItem{}
	}
	byKey := map[string]PersonalItem{}
	for _, r := range remote {
		byKey[r.Kind+"/"+r.Name] = r
	}
	keys := map[string]bool{}
	for k := range local {
		keys[k] = true
	}
	for k := range byKey {
		keys[k] = true
	}
	for k := range st.Items {
		keys[k] = true
	}
	for k := range skipped {
		keys[k] = true
	}
	var errs []error
	for _, key := range slices.Sorted(func(yield func(string) bool) {
		for k := range keys {
			if !yield(k) {
				return
			}
		}
	}) {
		kind, name, _ := strings.Cut(key, "/")
		if why, ok := skipped[key]; ok {
			rep.Skipped[key] = why
			continue
		}
		if !allowed[kind] {
			if _, mine := local[key]; mine {
				rep.Skipped[key] = "your organization doesn't keep your own " + PersonalKinds[kind]
			}
			continue
		}
		files, here := local[key]
		r, there := byKey[key]
		was, synced := st.Items[key]
		sum := ""
		if here {
			sum = agentpkg.Digest(files)
		}
		send := func(base int) {
			got, err := c.PutPersonal(ctx, kind, name, files, base)
			if err != nil {
				if ref, ok := errors.AsType[*Refusal](err); ok && ref.Status == http.StatusUnprocessableEntity {
					rep.Skipped[key] = ref.Message
					return
				}
				errs = append(errs, fmt.Errorf("%s: %w", key, err))
				return
			}
			st.Items[key] = syncedItem{got.Revision, got.SHA256}
			rep.Sent = append(rep.Sent, key)
		}
		fetch := func(into string) bool {
			it, err := c.PersonalItem(ctx, kind, name)
			if err == nil {
				err = writePersonal(into, kind, name, it.Files)
			}
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", key, err))
				return false
			}
			if into == root {
				st.Items[key] = syncedItem{it.Revision, it.SHA256}
			}
			return true
		}
		switch {
		case here && !there && synced && sum == was.SHA256:
			// Removed on another computer, unchanged here.
			if err := removePersonal(root, kind, name); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", key, err))
				continue
			}
			delete(st.Items, key)
			rep.Removed = append(rep.Removed, key)
		case here && !there:
			send(0)
		case !here && there && synced && was.Revision == r.Revision:
			// Removed here, unchanged on the service.
			if err := c.DeletePersonal(ctx, kind, name, r.Revision); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", key, err))
				continue
			}
			delete(st.Items, key)
			rep.Removed = append(rep.Removed, key)
		case !here && there:
			if fetch(root) {
				rep.Received = append(rep.Received, key)
			}
		case !here && !there:
			delete(st.Items, key)
		case sum == r.SHA256:
			st.Items[key] = syncedItem{r.Revision, r.SHA256}
		case synced && was.Revision == r.Revision:
			send(r.Revision) // changed here
		case synced && sum == was.SHA256:
			if fetch(root) { // changed on another computer
				rep.Received = append(rep.Received, key)
			}
		default:
			// Changed on both (or first met on both): theirs kept aside,
			// this computer's sent.
			if fetch(filepath.Join(root, ConflictsDir)) {
				rep.Conflicts = append(rep.Conflicts, key)
				send(r.Revision)
			}
		}
	}
	if b, err := json.MarshalIndent(st, "", "  "); err == nil {
		if err := os.MkdirAll(filepath.Dir(state), 0o700); err == nil {
			errs = append(errs, os.WriteFile(state, b, 0o600))
		}
	}
	return rep, errors.Join(errs...)
}

// writePersonal writes an asset's files under root, replacing what it
// had: only its own paths (an agent's one file, a skill's folder).
func writePersonal(root, kind, name string, files map[string]string) error {
	dir := PersonalKinds[kind]
	own := func(p string) bool {
		if strings.Contains(p, "..") || strings.HasPrefix(p, "/") || strings.Contains(p, "\\") {
			return false
		}
		switch kind {
		case "skill":
			return strings.HasPrefix(p, dir+"/"+name+"/")
		case "workflow":
			return p == dir+"/"+name+".flow.yaml"
		}
		return p == dir+"/"+name+".md"
	}
	for p := range files {
		if !own(p) {
			return fmt.Errorf("%s isn't one of %s's files", p, name)
		}
	}
	if err := removePersonal(root, kind, name); err != nil {
		return err
	}
	for p, text := range files {
		path := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// removePersonal removes an asset's files under root.
func removePersonal(root, kind, name string) error {
	dir := filepath.Join(root, PersonalKinds[kind])
	var err error
	switch kind {
	case "skill":
		err = os.RemoveAll(filepath.Join(dir, name))
	case "workflow":
		err = os.Remove(filepath.Join(dir, name+".flow.yaml"))
	default:
		err = os.Remove(filepath.Join(dir, name+".md"))
	}
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
