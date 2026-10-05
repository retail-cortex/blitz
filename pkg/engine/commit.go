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
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/textutil"
)

// CommitFile is one staged file: its git status letter (A, M, D, R, …)
// and workspace-relative path.
type CommitFile struct {
	Status string
	Path   string
}

// CommitDraft is what the commit dialog shows: the staged files and a
// message the model drafted from their diff ("" with Problem saying why
// when it couldn't).
type CommitDraft struct {
	Files   []CommitFile
	Message string
	Problem string
}

// ErrNothingStaged is Commit's error when the index matches HEAD.
var ErrNothingStaged = errors.New("nothing is staged to commit")

// ErrEmptyMessage is Commit's error without a message.
var ErrEmptyMessage = errors.New("a commit needs a message")

// commitTimeout bounds a commit, which runs the repository's hooks.
const commitTimeout = 2 * time.Minute

// maxCommitDiffChars is how much of the staged diff the model sees.
const maxCommitDiffChars = 24_000

// commitInstruction asks for a commit message from a staged diff.
const commitInstruction = `You write the git commit message for the staged changes of a software project, on the developer's behalf, from their diff. Reply with the message only: no Markdown fences, no commentary, no quotes around it.
First line: a summary of what the commit does and why, imperative mood ("Fix", "Add"), at most 72 characters, no trailing period. Follow the style of the recent commit subjects if they show one (a prefix such as "feat:" or "pkg/x:", capitalisation); otherwise plain.
Then, only if the change needs it, a blank line and a short body (wrapped at 72 characters) saying what changed and why, as plain sentences or "- " bullets. No body for a small, obvious change.
Describe only what the diff shows. Never repeat secrets, keys or credentials, even if the diff holds some.`

// DraftCommit lists what's staged (all of the workspace's changes staged
// first with stageAll, as git add --all) and has the model draft a commit
// message from the diff, secrets redacted. With nothing staged it returns
// no files and asks nothing; a model that fails leaves the message empty,
// with Problem saying why, so the user can still write one.
func (w *Workspace) DraftCommit(ctx context.Context, stageAll bool) (CommitDraft, error) {
	if !w.inRepository(ctx) {
		return CommitDraft{}, ErrNotARepository
	}
	if stageAll {
		if _, err := w.gitDo(ctx, nil, "add", "--all"); err != nil {
			return CommitDraft{}, err
		}
	}
	files, err := w.stagedFiles(ctx)
	if err != nil || len(files) == 0 {
		return CommitDraft{Files: files}, err
	}
	draft := CommitDraft{Files: files}
	msg, err := w.askCommitMessage(ctx)
	if err != nil {
		draft.Problem = err.Error()
		return draft, nil
	}
	draft.Message = msg
	return draft, nil
}

// stagedFiles lists the index's changes against HEAD (against nothing
// before the first commit).
func (w *Workspace) stagedFiles(ctx context.Context) ([]CommitFile, error) {
	out, err := w.gitDo(ctx, nil, "diff", "--cached", "--name-status", "-z", "--no-renames")
	if err != nil {
		return nil, err
	}
	var files []CommitFile
	fields := strings.Split(string(out), "\x00")
	for i := 0; i+1 < len(fields); i += 2 {
		if fields[i] == "" {
			break
		}
		files = append(files, CommitFile{Status: fields[i][:1], Path: fields[i+1]})
	}
	return files, nil
}

// askCommitMessage has the suggestions model (else the reviewer's, else
// the main one) write a message from the staged diff and the recent
// commit subjects.
func (w *Workspace) askCommitMessage(ctx context.Context) (string, error) {
	diff, err := w.gitDo(ctx, nil, "diff", "--cached", "--no-color", "--no-ext-diff", "--no-textconv", "--stat", "--patch")
	if err != nil {
		return "", err
	}
	text := string(diff)
	if len(text) > maxCommitDiffChars {
		text = text[:maxCommitDiffChars] + "\n[… the rest of the diff is cut]"
	}
	var b strings.Builder
	if log, err := w.gitDo(ctx, nil, "log", "-n", "10", "--format=%s"); err == nil && len(log) > 0 {
		b.WriteString("Recent commit subjects, newest first:\n")
		b.Write(log)
		b.WriteString("\n")
	}
	b.WriteString("The staged diff:\n")
	b.WriteString(SecretRedactor(w.cfg).String(text))
	llm, err := w.newModel(ctx, w.cfg, cmp.Or(w.cfg.Suggestions.Model, w.cfg.Permissions.Auto.Model))
	if err != nil {
		return "", err
	}
	reply, served, usage, err := runtime.Ask(ctx, llm, commitInstruction, b.String())
	if err != nil {
		return "", err
	}
	if usage != nil {
		slog.Info("commit message drafted", "model", served, "input_tokens", usage.PromptTokenCount, "output_tokens", usage.CandidatesTokenCount)
	}
	msg := cleanCommitMessage(reply)
	if msg == "" {
		return "", errors.New("the model returned no message")
	}
	return msg, nil
}

// cleanCommitMessage trims a drafted message: Markdown fences and quotes
// around it gone, trailing spaces off each line, at most 4,000
// characters.
func cleanCommitMessage(reply string) string {
	s := strings.TrimSpace(reply)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		if nl := strings.IndexByte(s, '\n'); nl >= 0 {
			s = s[nl+1:] // the fence's language
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '"' && s[len(s)-1] == '"' || s[0] == '\'' && s[len(s)-1] == '\'') && !strings.Contains(s, "\n") {
		s = s[1 : len(s)-1]
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t\r")
	}
	return textutil.Ellipsize(strings.TrimSpace(strings.Join(lines, "\n")), 4000)
}

// Commit commits what's staged with message (git commit -F -, the
// repository's hooks run) and returns the new commit's short hash and
// subject.
func (w *Workspace) Commit(ctx context.Context, message string) (hash, subject string, err error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return "", "", ErrEmptyMessage
	}
	if !w.inRepository(ctx) {
		return "", "", ErrNotARepository
	}
	files, err := w.stagedFiles(ctx)
	if err != nil {
		return "", "", err
	}
	if len(files) == 0 {
		return "", "", ErrNothingStaged
	}
	if _, err := w.gitDoFor(ctx, commitTimeout, []byte(message+"\n"), "commit", "-F", "-"); err != nil {
		return "", "", fmt.Errorf("git commit: %w", err)
	}
	out, err := w.gitDo(ctx, nil, "log", "-1", "--format=%h%x00%s")
	if err != nil {
		return "", "", err
	}
	hash, subject, _ = strings.Cut(strings.TrimSpace(string(out)), "\x00")
	return hash, subject, nil
}
