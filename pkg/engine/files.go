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

package engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/retail-cortex/blitz/pkg/engine/tools"
)

// The workspace's files for the user (the desktop app's explorer and
// editor, spec_files_029 §3–4): paths are relative to the workspace, with
// "/" separators.

// FileKind is what a directory entry is.
type FileKind string

// The kinds of entry ListDir reports; a symlink to a folder in the
// workspace is reported as a folder.
const (
	KindFile    FileKind = "file"
	KindFolder  FileKind = "folder"
	KindSymlink FileKind = "symlink"
)

// FileEntry is one entry of a folder.
type FileEntry struct {
	Name     string
	Path     string
	Kind     FileKind
	Size     int64
	Modified time.Time
	// Git is the entry's git status ("modified", "added", "renamed",
	// "untracked", "conflicted"; "changed" for a folder with changes
	// under it), "" when clean or outside a repository.
	Git string
	// Hidden says why the entry is hidden by default: "blocked" (the
	// agent's blocked paths), "ignored" (git), "dot", or "".
	Hidden string
	// AgentRule is the agent's rule for the path: "blocked", "read_only"
	// or "".
	AgentRule string
}

// DirListing is a folder's entries.
type DirListing struct {
	Entries   []FileEntry
	Truncated bool
	// Repo: the workspace is in a git repository.
	Repo bool
}

// FileContent is a file as the editor opens it.
type FileContent struct {
	Path      string
	Text      string
	Version   string
	Size      int64
	Binary    bool
	TooLarge  bool
	AgentRule string
}

// FileChangedError: a write's version doesn't match the file's (it was
// changed since it was read, or it exists when it was expected not to).
type FileChangedError struct {
	Path string
	// Current is the file's version now ("" when it doesn't exist).
	Current string
}

// Error says the file changed, or was deleted, since it was opened.
func (e *FileChangedError) Error() string {
	if e.Current == "" {
		return e.Path + " was deleted since it was opened"
	}
	return e.Path + " was changed since it was opened"
}

const (
	maxListed      = 5000
	maxEditorBytes = 2 << 20
	maxWalked      = 50000
)

// FileVersion is the version of content: its SHA-256.
func FileVersion(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (w *Workspace) files() *tools.Workspace { return w.tools.Workspace() }

// ErrBadPath: a path outside the workspace, or in .git.
var ErrBadPath = errors.New("not a path in the workspace")

// userPath checks a path from the user; it returns the OS form and the
// "/" form.
func userPath(p string) (string, string, error) {
	rel, err := tools.UserPath(p)
	if err != nil {
		return "", "", fmt.Errorf("%w: %w", ErrBadPath, err)
	}
	return rel, filepath.ToSlash(rel), nil
}

// ListDir lists the folder p (the workspace for ""), folders first.
// Hidden entries are left out unless showHidden.
func (w *Workspace) ListDir(ctx context.Context, p string, showHidden bool) (DirListing, error) {
	rel, slash, err := userPath(p)
	if err != nil {
		return DirListing{}, err
	}
	fsys := w.files()
	entries, err := fsys.UserReadDir(rel)
	if err != nil {
		return DirListing{}, err
	}
	g := w.gitInfo(ctx, slash)
	out := DirListing{Repo: g.repo}
	for _, e := range entries {
		if rel == "." && e.Name() == ".git" {
			continue
		}
		child := path.Join(slash, e.Name())
		if slash == "." {
			child = e.Name()
		}
		entry := FileEntry{Name: e.Name(), Path: child, Kind: KindFile}
		if info, err := e.Info(); err == nil {
			entry.Size, entry.Modified = info.Size(), info.ModTime()
			switch {
			case info.Mode()&fs.ModeSymlink != 0:
				entry.Kind = KindSymlink
				// A link to a folder in the workspace opens as a folder.
				if t, err := fsys.UserStat(filepath.FromSlash(child)); err == nil && t.IsDir() {
					entry.Kind = KindFolder
				}
			case info.IsDir():
				entry.Kind = KindFolder
				entry.Size = 0
			}
		}
		entry.AgentRule = fsys.AgentRule(filepath.FromSlash(child))
		if entry.AgentRule == "blocked" {
			entry.Hidden = "blocked"
		} else if strings.HasPrefix(entry.Name, ".") {
			entry.Hidden = "dot"
		}
		entry.Git = g.status(child, entry.Kind == KindFolder)
		out.Entries = append(out.Entries, entry)
	}
	if g.repo {
		ignored := w.gitIgnored(ctx, out.Entries)
		for i := range out.Entries {
			if out.Entries[i].Hidden == "" && ignored[out.Entries[i].Path] {
				out.Entries[i].Hidden = "ignored"
			}
		}
	}
	if !showHidden {
		out.Entries = slices.DeleteFunc(out.Entries, func(e FileEntry) bool { return e.Hidden != "" })
	}
	sort.SliceStable(out.Entries, func(i, j int) bool {
		a, b := out.Entries[i], out.Entries[j]
		if (a.Kind == KindFolder) != (b.Kind == KindFolder) {
			return a.Kind == KindFolder
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	if len(out.Entries) > maxListed {
		out.Entries, out.Truncated = out.Entries[:maxListed], true
	}
	return out, nil
}

// ReadFile opens the file p for the editor: binary files and files over
// the editor's limit come without their text.
func (w *Workspace) ReadFile(p string) (FileContent, error) {
	rel, slash, err := userPath(p)
	if err != nil {
		return FileContent{}, err
	}
	limit := int64(maxEditorBytes)
	if m := w.cfg.Tools.MaxFileSizeBytes; m > 0 && m < limit {
		limit = m
	}
	fsys := w.files()
	data, info, err := fsys.UserReadFile(rel, limit)
	out := FileContent{Path: slash, AgentRule: fsys.AgentRule(rel)}
	if info != nil {
		out.Size = info.Size()
	}
	switch {
	case errors.Is(err, tools.ErrTooLarge):
		out.TooLarge = true
		return out, nil
	case err != nil:
		return FileContent{}, err
	}
	out.Version = FileVersion(data)
	if isBinary(data) {
		out.Binary = true
		return out, nil
	}
	out.Text = string(data)
	return out, nil
}

// isBinary: a NUL in the first 8 kB, or not UTF-8.
func isBinary(data []byte) bool {
	head := data
	if len(head) > 8192 {
		head = head[:8192]
	}
	if bytes.IndexByte(head, 0) >= 0 {
		return true
	}
	return !utf8.Valid(data)
}

// WriteFile writes text to p if the file still has version ("" when it
// must not exist yet), and returns the new version. Otherwise it returns
// a *FileChangedError.
func (w *Workspace) WriteFile(ctx context.Context, p, text, version string) (string, error) {
	rel, slash, err := userPath(p)
	if err != nil {
		return "", err
	}
	created := false
	data := []byte(text)
	err = w.files().UserWriteFile(ctx, rel, data, func(current []byte, exists bool) error {
		now := ""
		if exists {
			now = FileVersion(current)
		}
		if now != version {
			return &FileChangedError{Path: slash, Current: now}
		}
		created = !exists
		return nil
	})
	if err != nil {
		return "", err
	}
	w.userEdited(slash, map[bool]string{true: "created", false: "edited"}[created])
	return FileVersion(data), nil
}

// CreateFolder creates the folder p.
func (w *Workspace) CreateFolder(p string) error {
	rel, slash, err := userPath(p)
	if err != nil {
		return err
	}
	if err := w.files().UserMkdir(rel); err != nil {
		return err
	}
	w.userEdited(slash, "created")
	return nil
}

// RenameFile moves from to to (a file or a folder); to must not exist.
func (w *Workspace) RenameFile(ctx context.Context, from, to string) error {
	fromRel, fromSlash, err := userPath(from)
	if err != nil {
		return err
	}
	toRel, toSlash, err := userPath(to)
	if err != nil {
		return err
	}
	if err := w.files().UserRename(ctx, fromRel, toRel); err != nil {
		return err
	}
	w.userEdited(fromSlash, "renamed to "+toSlash)
	return nil
}

// DeleteFile deletes p, a folder with everything in it.
func (w *Workspace) DeleteFile(ctx context.Context, p string) error {
	rel, slash, err := userPath(p)
	if err != nil {
		return err
	}
	if err := w.files().UserRemove(ctx, rel); err != nil {
		return err
	}
	w.userEdited(slash, "deleted")
	return nil
}

// StatFiles returns each file's version, "" for one that's gone (or isn't
// a readable file). Files over the editor's limit get their size and time
// instead of a hash.
func (w *Workspace) StatFiles(paths []string) map[string]string {
	out := make(map[string]string, len(paths))
	for _, p := range paths {
		rel, slash, err := userPath(p)
		if err != nil {
			continue
		}
		data, info, err := w.files().UserReadFile(rel, maxEditorBytes)
		switch {
		case err == nil:
			out[slash] = FileVersion(data)
		case errors.Is(err, tools.ErrTooLarge):
			out[slash] = fmt.Sprintf("size:%d:%d", info.Size(), info.ModTime().UnixNano())
		default:
			out[slash] = ""
		}
	}
	return out
}

// ---- The agent learns of the user's edits (FIL-20).

type userEdit struct{ path, what string }

// userEdited records a change the user made, for the agent's next turn.
func (w *Workspace) userEdited(p, what string) {
	w.editsMu.Lock()
	defer w.editsMu.Unlock()
	w.userEdits = slices.DeleteFunc(w.userEdits, func(e userEdit) bool { return e.path == p })
	w.userEdits = append(w.userEdits, userEdit{p, what})
}

// takeUserEdits returns the note about the user's edits since the last
// turn, and forgets them.
func (w *Workspace) takeUserEdits() string {
	w.editsMu.Lock()
	edits := w.userEdits
	w.userEdits = nil
	w.editsMu.Unlock()
	if len(edits) == 0 {
		return ""
	}
	parts := make([]string, len(edits))
	for i, e := range edits {
		parts[i] = e.path + " (" + e.what + ")"
	}
	return "The user changed these files in the editor since your last turn: " + strings.Join(parts, ", ") +
		". Read them again before relying on what you saw."
}

const userEditsTag = "user-edits"

// withUserEdits adds the note about the user's edits to a prompt.
func withUserEdits(prompt, note string) string {
	if note == "" {
		return prompt
	}
	return prompt + "\n\n<" + userEditsTag + ">" + note + "</" + userEditsTag + ">"
}

// DisplayPrompt is a prompt as the user wrote it: without the note about
// their edits (withUserEdits), which is for the agent.
func DisplayPrompt(text string) string {
	open := "\n\n<" + userEditsTag + ">"
	i := strings.Index(text, open)
	if i < 0 {
		return text
	}
	end := strings.Index(text[i:], "</"+userEditsTag+">")
	if end < 0 {
		return text
	}
	return text[:i] + text[i+end+len("</"+userEditsTag+">"):]
}

// ---- Git: status and ignored files, running none of the repository's
// configured programs (filters, fsmonitor), as GitDiff.

type gitStatus struct {
	repo  bool
	files map[string]string // workspace-relative path → status
}

// status is the status of path (a folder: whether anything under it changed).
func (g gitStatus) status(p string, folder bool) string {
	if !folder {
		return g.files[p]
	}
	prefix := p + "/"
	for f := range g.files {
		if strings.HasPrefix(f, prefix) {
			return "changed"
		}
	}
	return ""
}

func (w *Workspace) git(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	full := append([]string{"-c", "core.fsmonitor=false"}, noFilters(ctx, w.Dir())...)
	cmd := exec.CommandContext(ctx, "git", append(full, args...)...)
	cmd.Dir = w.Dir()
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	return cmd.Output()
}

// gitInfo is the status of the files under the folder dir.
func (w *Workspace) gitInfo(ctx context.Context, dir string) gitStatus {
	prefix, err := w.git(ctx, nil, "rev-parse", "--show-prefix")
	if err != nil {
		return gitStatus{}
	}
	top := strings.TrimSpace(string(prefix)) // the workspace, from the repository's top
	g := gitStatus{repo: true, files: map[string]string{}}
	out, err := w.git(ctx, nil, "--literal-pathspecs", "status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignored=no", "--", dir)
	if err != nil {
		return g
	}
	recs := strings.Split(string(out), "\x00")
	for i := 0; i < len(recs); i++ {
		r := recs[i]
		if len(r) < 4 {
			continue
		}
		xy, p := r[:2], r[3:]
		if xy[0] == 'R' || xy[0] == 'C' {
			i++ // the original path follows
		}
		rel, ok := strings.CutPrefix(p, top)
		if !ok {
			continue
		}
		g.files[rel] = gitState(xy)
	}
	return g
}

// gitState names a porcelain XY status.
func gitState(xy string) string {
	switch {
	case xy == "??":
		return "untracked"
	case xy == "DD" || xy == "AA" || strings.ContainsRune(xy, 'U'):
		return "conflicted"
	case xy[0] == 'R' || xy[1] == 'R':
		return "renamed"
	case xy[0] == 'A':
		return "added"
	case xy[0] == 'D' || xy[1] == 'D':
		return "deleted"
	}
	return "modified"
}

// gitIgnored reports which entries git ignores.
func (w *Workspace) gitIgnored(ctx context.Context, entries []FileEntry) map[string]bool {
	var in bytes.Buffer
	for _, e := range entries {
		in.WriteString(e.Path)
		if e.Kind == KindFolder {
			in.WriteByte('/')
		}
		in.WriteByte(0)
	}
	out, _ := w.git(ctx, in.Bytes(), "check-ignore", "-z", "--stdin") // exit 1: none ignored
	ignored := map[string]bool{}
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			ignored[strings.TrimSuffix(p, "/")] = true
		}
	}
	return ignored
}

// ---- Go to file (FIL-17).

type fileIndex struct {
	mu    sync.Mutex
	at    time.Time
	paths []string
}

// FindFiles returns up to limit files whose path matches query, best
// first. Hidden files are left out.
func (w *Workspace) FindFiles(ctx context.Context, query string, limit int) ([]string, error) {
	return w.FindPaths(ctx, query, limit, false)
}

// FindPaths is FindFiles, with folders also the folders the files are in
// ("src/cart/", with a trailing slash), matched the same way.
func (w *Workspace) FindPaths(ctx context.Context, query string, limit int, folders bool) ([]string, error) {
	paths, err := w.allFiles(ctx)
	if err != nil {
		return nil, err
	}
	if folders {
		paths = withFolders(paths)
	}
	if limit <= 0 {
		limit = 50
	}
	type hit struct {
		p     string
		score int
	}
	var hits []hit
	for _, p := range paths {
		if s, ok := fuzzyScore(query, p); ok {
			hits = append(hits, hit{p, s})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return len(hits[i].p) < len(hits[j].p)
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.p
	}
	return out, nil
}

// withFolders adds the folders of paths (each once, with a trailing slash).
func withFolders(paths []string) []string {
	seen := map[string]bool{}
	out := slices.Clone(paths)
	for _, p := range paths {
		for d := path.Dir(p); d != "." && d != "/" && !seen[d]; d = path.Dir(d) {
			seen[d] = true
			out = append(out, d+"/")
		}
	}
	return out
}

// allFiles lists the workspace's files that aren't hidden, kept for 5 seconds.
func (w *Workspace) allFiles(ctx context.Context) ([]string, error) {
	w.index.mu.Lock()
	defer w.index.mu.Unlock()
	if time.Since(w.index.at) < 5*time.Second && w.index.paths != nil {
		return w.index.paths, nil
	}
	var paths []string
	if out, err := w.git(ctx, nil, "ls-files", "--cached", "--others", "--exclude-standard", "--deduplicate", "-z"); err == nil {
		for _, p := range strings.Split(string(out), "\x00") {
			if p != "" && len(paths) < maxWalked {
				paths = append(paths, p)
			}
		}
	} else {
		var werr error
		paths, werr = w.walkFiles()
		if werr != nil {
			return nil, werr
		}
	}
	fsys := w.files()
	paths = slices.DeleteFunc(paths, func(p string) bool {
		for _, seg := range strings.Split(p, "/") {
			if strings.HasPrefix(seg, ".") {
				return true
			}
		}
		if _, err := fsys.UserStat(filepath.FromSlash(p)); err != nil {
			return true // deleted, but still in git's index
		}
		return fsys.AgentRule(filepath.FromSlash(p)) == "blocked"
	})
	w.index.paths, w.index.at = paths, time.Now()
	return paths, nil
}

// walkFiles lists the files outside a repository, skipping dot folders.
func (w *Workspace) walkFiles() ([]string, error) {
	var paths []string
	root, err := os.OpenRoot(w.Dir())
	if err != nil {
		return nil, err
	}
	defer root.Close()
	err = fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != "." && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if len(paths) >= maxWalked {
			return fs.SkipAll
		}
		paths = append(paths, p)
		return nil
	})
	return paths, err
}

// fuzzyScore matches query's characters in order in p, ignoring case and
// spaces. Runs of consecutive characters and characters at the start of a
// name or after a separator score higher, and so does a match that starts
// the file's name (more if it is the whole name but its extension).
func fuzzyScore(query, p string) (int, bool) {
	q := strings.ToLower(strings.ReplaceAll(query, " ", ""))
	if q == "" {
		return 0, true
	}
	lp := strings.ToLower(p)
	score, ok := matchFrom(q, lp)
	if !ok {
		return 0, false
	}
	name := lp[strings.LastIndex(lp, "/")+1:]
	if strings.HasPrefix(name, q[:1]) {
		if s, ok := matchFrom(q, name); ok {
			s += 20
			if stem, _, _ := strings.Cut(name, "."); stem == q {
				s += 15
			}
			score = max(score, s)
		}
	}
	return score, true
}

func matchFrom(q, lp string) (int, bool) {
	score, qi, prev := 0, 0, -2
	for i := 0; i < len(lp) && qi < len(q); i++ {
		if lp[i] != q[qi] {
			continue
		}
		score++
		if i == prev+1 {
			score += 5
		}
		if i == 0 || strings.ContainsRune("/_-. ", rune(lp[i-1])) {
			score += 8
		}
		prev = i
		qi++
	}
	if qi < len(q) {
		return 0, false
	}
	return score - len(lp)/16, true
}
