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
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/agentpkg"
	"github.com/retail-cortex/blitz/pkg/atomicfile"
	"github.com/retail-cortex/blitz/pkg/secretscan"
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

// PersonalSecrets are what look like secrets in one of the person's own
// assets, by file and line, before it leaves the computer (shared or
// synced): what's published is read for secrets first, as a shared folder
// is (spec_053 EN-36a, EN-66). A line marked secretscan.AllowMarker is a
// false alarm.
func PersonalSecrets(files map[string]string) []secretscan.Finding {
	var out []secretscan.Finding
	for _, name := range slices.Sorted(func(yield func(string) bool) {
		for k := range files {
			if !yield(k) {
				return
			}
		}
	}) {
		out = append(out, secretscan.Scan(name, []byte(files[name]))...)
	}
	return out
}

// SecretsSummary names what a secret scan found, each by file and line,
// the first ten.
func SecretsSummary(found []secretscan.Finding) string {
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

// PersonalFiles are the person's own assets under root (~/.blitz), by
// "kind/name", each its files by path under root; skipped are those that
// can't sync, and why (agentpkg.ReadAssets).
func PersonalFiles(root string) (map[string]map[string]string, map[string]string) {
	kinds := make([]string, 0, len(PersonalKinds))
	for k := range PersonalKinds {
		kinds = append(kinds, k)
	}
	return agentpkg.ReadAssets(root, kinds...)
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

// syncState is what the last sync left, by "kind/name", and whose sync it
// was (the person's ID on the service).
type syncState struct {
	Person string                `json:"person,omitempty"`
	Items  map[string]syncedItem `json:"items"`
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
	if _, err := atomicfile.ReadJSONOrQuarantine(state, &st); err != nil {
		if _, statErr := os.Stat(state); !errors.Is(statErr, fs.ErrNotExist) {
			return rep, fmt.Errorf("reading sync state: %w", err)
		}
		slog.Warn("personal sync state was damaged and quarantined; starting from empty state", "path", state, "error", err)
	}
	// The last sync was another person's (another account on the same
	// service), or remembers items the service no longer lists any of (a
	// reset): nothing here is taken for removed elsewhere, and what's here
	// is sent (EN-93b; security review enterprise#3).
	person := c.Status().Person.ID
	if st.Person != person || len(remote) == 0 && len(st.Items) > 0 {
		st.Items = nil
	}
	st.Person = person
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
			if found := PersonalSecrets(files); len(found) > 0 {
				rep.Skipped[key] = fmt.Sprintf("not sent: it holds what look like secrets: %s. Remove them, or mark a false alarm with %s on its line", SecretsSummary(found), secretscan.AllowMarker)
				return
			}
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
	if err := atomicfile.WriteJSON(state, st, 0o600); err != nil {
		errs = append(errs, fmt.Errorf("saving sync state: %w", err))
	}
	return rep, errors.Join(errs...)
}

// writePersonal writes an asset's files under root, replacing what it
// had: only its own paths (an agent's one file, a skill's folder).
func writePersonal(root, kind, name string, files map[string]string) error {
	if err := agentpkg.WriteAsset(root, kind, name, files); err != nil {
		return fmt.Errorf("%w: not one of %s's files", err, name)
	}
	return nil
}

// removePersonal removes an asset's files under root.
func removePersonal(root, kind, name string) error { return agentpkg.RemoveAsset(root, kind, name) }
