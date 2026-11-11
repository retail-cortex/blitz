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

package tools

import (
	"bufio"
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/pdftext"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// maxReadOutputBytes caps how much file content read_file returns in one call.
const maxReadOutputBytes = 512 * 1024

// wholeFileLines is the longest file read_file returns whole when asked
// for a range of it: models page short files in small pieces, a call each
// (153 of 207 ranged reads were of files under 400 lines, 2026-11-10).
const wholeFileLines = 400

// readFileDescription tells the model how to read without reading again.
const readFileDescription = "Read a text file with line numbers. Read the whole file: a file of up to 400 lines comes whole even when you ask for a range, so give start_line/end_line only to page a longer one. Don't read a file again when its contents are already in this conversation: a read of a file that hasn't changed since you were shown it answers unchanged (pass force to have it again)."

// ReadFileInput defines arguments for reading a file.
type ReadFileInput struct {
	Path      string `json:"path" jsonschema:"The absolute or workspace-relative path to the file"`
	StartLine int    `json:"start_line,omitempty" jsonschema:"Optional 1-indexed starting line number"`
	EndLine   int    `json:"end_line,omitempty" jsonschema:"Optional 1-indexed ending line number"`
	Force     bool   `json:"force,omitempty" jsonschema:"Return the content even when this conversation already shows it unchanged"`
}

// ReadFileOutput holds the result of reading a file.
type ReadFileOutput struct {
	Path       string `json:"path"`
	Content    string `json:"content"`
	TotalLines int    `json:"total_lines"`
	Truncated  bool   `json:"truncated,omitempty"`
	Lines      string `json:"lines,omitempty"`  // the lines returned, "1-220"
	Digest     string `json:"digest,omitempty"` // the file's version: an unchanged one reads unchanged (FS-83)
	Unchanged  bool   `json:"unchanged,omitempty"`
	Note       string `json:"note,omitempty"`
	Error      string `json:"error,omitempty"`
}

// NewReadFileTool creates an ADK tool for reading files.
func NewReadFileTool(ws *Workspace) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name:        "read_file",
			Description: readFileDescription,
		},
		func(ctx agent.Context, input ReadFileInput) (ReadFileOutput, error) {
			if strings.HasSuffix(strings.ToLower(input.Path), ".ipynb") && input.StartLine == 0 && input.EndLine == 0 {
				if out, ok := readNotebook(ws, input.Path); ok {
					return out, nil
				}
			}
			if pdftext.IsPDFPath(input.Path) {
				if out, ok := readPDF(ctx, ws, input); ok {
					return out, nil
				}
			}
			out, read, err := readFileLines(ws, input)
			if err != nil {
				return ReadFileOutput{Path: input.Path, Error: fmt.Sprintf("failed to read file: %v", err)}, nil
			}
			if out.Truncated {
				return out, nil
			}
			if key, ok := ReadKey(ctx); ok && !input.Force {
				if seen, ok := ws.reads.covering(key, read.digest, read.start, read.end); ok {
					return unchangedRead(out, seen), nil
				}
			}
			out.Lines, out.Digest = fmt.Sprintf("%d-%d", read.start, read.end), read.digest
			return out, nil
		},
	)
}

// unchangedRead is the answer to a read of lines the agent was already
// shown, the file unchanged since (FS-83): no content, and where it is.
func unchangedRead(out ReadFileOutput, seen fileRead) ReadFileOutput {
	return ReadFileOutput{
		Path:       out.Path,
		TotalLines: out.TotalLines,
		Unchanged:  true,
		Note:       fmt.Sprintf("Unchanged since your earlier read of lines %d-%d in this conversation: use that result, it's still there. Pass force: true only if you can't find it.", seen.start, seen.end),
	}
}

// readFileLines streams the file once, keeping only the requested line
// range, and says which lines of which version of the file it returned.
func readFileLines(ws *Workspace, input ReadFileInput) (ReadFileOutput, fileRead, error) {
	rel, err := ws.Rel(input.Path)
	if err != nil {
		return ReadFileOutput{}, fileRead{}, err
	}
	f, err := ws.Open(rel)
	if errors.Is(err, fs.ErrNotExist) {
		if near := nearPaths(ws, rel); len(near) > 0 {
			err = fmt.Errorf("%w; did you mean %s?", err, strings.Join(near, ", "))
		}
	}
	if err != nil {
		return ReadFileOutput{}, fileRead{}, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return ReadFileOutput{}, fileRead{}, err
	}
	if info.IsDir() {
		return ReadFileOutput{}, fileRead{}, fmt.Errorf("%s is a directory", input.Path)
	}
	if info.Size() > ws.MaxFileSize() {
		return ReadFileOutput{}, fileRead{}, fmt.Errorf("file is %d bytes, exceeding the %d byte limit (tools.max_file_size_bytes); use grep or run_shell_command with head/tail instead", info.Size(), ws.MaxFileSize())
	}
	sum := sha256.New()
	sum.Write([]byte(rel + "\x00")) // the same content in another file is another file
	src := bufio.NewReaderSize(io.TeeReader(io.LimitReader(f, ws.MaxFileSize()), sum), 64*1024)
	if head, _ := src.Peek(8192); isBinary(head) {
		return ReadFileOutput{}, fileRead{}, errors.New(binaryHint(input.Path, head))
	}
	out, start, end, err := pageLines(src, input)
	if err != nil {
		return ReadFileOutput{}, fileRead{}, err
	}
	return out, fileRead{digest: hex.EncodeToString(sum.Sum(nil))[:16], start: start, end: end}, nil
}

// maxNearPaths is how many files a read of a missing one suggests, and
// maxNearWalk how many entries it looks through to find them.
const (
	maxNearPaths = 5
	maxNearWalk  = 20000
)

// nearPaths is up to five workspace files a missing path may have meant:
// those with its name first, then those whose name holds its stem, the
// shortest paths first. Hidden folders and node_modules aren't searched.
func nearPaths(ws *Workspace, rel string) []string {
	base := strings.ToLower(path.Base(filepath.ToSlash(rel)))
	stem := strings.TrimSuffix(base, path.Ext(base))
	if stem == "" {
		return nil
	}
	var same, like []string
	seen := 0
	_ = ws.Walk(".", func(e WalkEntry) error {
		if e.IsRoot {
			return nil
		}
		if seen++; seen > maxNearWalk {
			return fs.SkipAll
		}
		name := e.Entry.Name()
		if e.Entry.IsDir() {
			if strings.HasPrefix(name, ".") || name == "node_modules" {
				return fs.SkipDir
			}
			return nil
		}
		switch lower := strings.ToLower(name); {
		case lower == base:
			same = append(same, e.Path)
		case strings.Contains(lower, stem):
			like = append(like, e.Path)
		}
		return nil
	})
	byLength := func(a, b string) int { return cmp.Or(len(a)-len(b), strings.Compare(a, b)) }
	slices.SortFunc(same, byLength)
	slices.SortFunc(like, byLength)
	near := append(same, like...)
	return near[:min(len(near), maxNearPaths)]
}

// isBinary reports whether a file's start is binary data, not text: it
// holds a NUL byte, which text never does.
func isBinary(head []byte) bool { return bytes.IndexByte(head, 0) >= 0 }

// binaryHint is why read_file won't read a binary file, and what reads it:
// its bytes as lines would be noise, and a small model takes what text is
// in them (a database's rows) for the answer (2026-11-08).
func binaryHint(path string, head []byte) string {
	switch {
	case bytes.HasPrefix(head, []byte("SQLite format 3\x00")):
		return fmt.Sprintf("%s is a SQLite database, not text: query it with its connection's db__ tools, or sqlite3 -readonly %s in run_shell_command (.tables, .schema)", path, path)
	case bytes.HasPrefix(head, []byte("\x89PNG")), bytes.HasPrefix(head, []byte("\xff\xd8\xff")), bytes.HasPrefix(head, []byte("GIF8")), bytes.HasPrefix(head, []byte("RIFF")):
		return fmt.Sprintf("%s is a picture, not text: look at it with view_image", path)
	}
	return fmt.Sprintf("%s is a binary file, not text: read_file reads text files", path)
}

// pageLines numbers r's lines, keeping the requested range, and returns
// the first and last line it gave. A range of a file of up to
// wholeFileLines lines gives the whole file.
func pageLines(src io.Reader, input ReadFileInput) (ReadFileOutput, int, int, error) {
	start := 1
	if input.StartLine > 1 {
		start = input.StartLine
	}
	end := input.EndLine // 0 means "to EOF"
	if end > 0 && end < start {
		end = start
	}
	ranged := start > 1 || end > 0
	var whole strings.Builder // a ranged read's lines while the file may be short

	var (
		sb        strings.Builder
		lineNo    int
		truncated bool
		numBuf    []byte
		lastLine  string
	)
	writeLine := func(n int, text string) bool {
		if sb.Len()+len(text)+8 > maxReadOutputBytes {
			return false
		}
		numberLine(&sb, &numBuf, n, text)
		return true
	}
	r := bufio.NewReaderSize(src, 64*1024)
	for {
		line, readErr := r.ReadString('\n')
		if readErr != nil && readErr != io.EOF {
			return ReadFileOutput{}, 0, 0, readErr
		}
		// A trailing "\n" yields a final empty line, matching strings.Split semantics.
		lineNo++
		text := strings.TrimSuffix(line, "\n")
		inRange := lineNo >= start && (end <= 0 || lineNo <= end)
		if inRange && !truncated && !writeLine(lineNo, text) {
			truncated = true
		}
		if ranged && lineNo <= wholeFileLines && whole.Len()+len(text)+8 <= maxReadOutputBytes {
			numberLine(&whole, &numBuf, lineNo, text)
		}
		if readErr == io.EOF {
			lastLine = text
			break
		}
	}
	totalLines := lineNo

	if ranged && totalLines <= wholeFileLines && whole.Len() > 0 && !truncated {
		return ReadFileOutput{
			Path:       input.Path,
			Content:    whole.String(),
			TotalLines: totalLines,
			Note:       fmt.Sprintf("The file is %d lines, so here it is whole.", totalLines),
		}, 1, totalLines, nil
	}

	// Out-of-range starts clamp to the last line.
	if start > totalLines {
		start = totalLines
		if !writeLine(totalLines, lastLine) {
			truncated = true
		}
	}
	if end <= 0 || end > totalLines {
		end = totalLines
	}

	content := sb.String()
	if truncated {
		content += fmt.Sprintf("\n... [output truncated at %d bytes; use start_line/end_line to page]\n", maxReadOutputBytes)
	}
	return ReadFileOutput{
		Path:       input.Path,
		Content:    content,
		TotalLines: totalLines,
		Truncated:  truncated,
	}, start, end, nil
}

// numberLine writes line n of a file as read_file shows it: "%4d: text".
func numberLine(sb *strings.Builder, numBuf *[]byte, n int, text string) {
	*numBuf = strconv.AppendInt((*numBuf)[:0], int64(n), 10)
	for pad := 4 - len(*numBuf); pad > 0; pad-- {
		sb.WriteByte(' ')
	}
	sb.Write(*numBuf)
	sb.WriteString(": ")
	sb.WriteString(text)
	sb.WriteByte('\n')
}

// FileEntry represents a file in directory listing.
type FileEntry struct {
	Path  string `json:"path"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size_bytes"`
}

// ListFilesInput defines arguments for listing directory files.
type ListFilesInput struct {
	Directory  string `json:"directory,omitempty" jsonschema:"Directory path relative to workspace or absolute"`
	Recursive  bool   `json:"recursive,omitempty" jsonschema:"Whether to list files recursively"`
	MaxEntries int    `json:"max_entries,omitempty" jsonschema:"Maximum number of file entries to return"`
}

// ListFilesOutput holds listed files.
type ListFilesOutput struct {
	Directory  string      `json:"directory"`
	Files      []FileEntry `json:"files"`
	TotalCount int         `json:"total_count"`
	// Truncated says the listing stopped at max_entries: there may be more.
	Truncated bool   `json:"truncated,omitempty"`
	Error     string `json:"error,omitempty"`
}

// NewListFilesTool creates an ADK tool for listing directory files.
func NewListFilesTool(ws *Workspace) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name:        "list_files",
			Description: "List files and subdirectories within a directory path",
		},
		func(ctx agent.Context, input ListFilesInput) (ListFilesOutput, error) {
			dirPath := input.Directory
			if dirPath == "" {
				dirPath = "."
			}
			max := input.MaxEntries
			if max <= 0 {
				max = 200
			}
			max = min(max, 500)
			truncated := false

			files := make([]FileEntry, 0, 32)
			walkErr := ws.Walk(dirPath, func(e WalkEntry) error {
				if e.IsRoot {
					return nil
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
				d := e.Entry
				name := d.Name()
				if strings.HasPrefix(name, ".") {
					if d.IsDir() {
						return fs.SkipDir
					}
					return nil
				}

				var size int64
				if info, err := d.Info(); err == nil {
					size = info.Size()
				}
				files = append(files, FileEntry{Path: e.Path, IsDir: d.IsDir(), Size: size})

				if len(files) >= max {
					truncated = true
					return fs.SkipAll
				}
				if !input.Recursive && d.IsDir() {
					return fs.SkipDir
				}
				return nil
			})

			if walkErr != nil {
				return ListFilesOutput{Directory: dirPath, Error: fmt.Sprintf("failed to list directory: %v", walkErr)}, nil
			}

			return ListFilesOutput{
				Directory:  dirPath,
				Files:      files,
				TotalCount: len(files),
				Truncated:  truncated,
			}, nil
		},
	)
}

// CreateFileInput defines arguments for creating a file.
type CreateFileInput struct {
	Path      string `json:"path" jsonschema:"The path to create"`
	Content   string `json:"content" jsonschema:"The complete content to write"`
	Overwrite bool   `json:"overwrite,omitempty" jsonschema:"Whether to overwrite if file already exists"`
}

// CreateFileOutput holds file creation result.
type CreateFileOutput struct {
	Path         string `json:"path"`
	Success      bool   `json:"success"`
	BytesWritten int    `json:"bytes_written"`
	Error        string `json:"error,omitempty"`
}

// NewCreateFileTool creates an ADK tool for creating files.
func NewCreateFileTool(ws *Workspace, hooks *Hooks) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name:        "create_file",
			Description: "Create a new file with the specified content",
		},
		func(ctx agent.Context, input CreateFileInput) (CreateFileOutput, error) {
			fail := func(msg string) (CreateFileOutput, error) {
				return CreateFileOutput{Path: input.Path, Error: msg}, nil
			}
			rel, err := ws.WritablePath(input.Path)
			if err != nil {
				return fail(err.Error())
			}
			if rel == "." {
				return fail("path must name a file")
			}

			unlock, err := ws.lockPaths(ctx, rel)
			if err != nil {
				return fail(err.Error())
			}
			defer unlock()

			verb, before := "Create", ""
			existing, readErr := ws.ReadFile(rel)
			existed := readErr == nil
			if existed {
				if !input.Overwrite {
					return fail(fmt.Sprintf("file '%s' already exists; set overwrite=true to overwrite", input.Path))
				}
				verb, before = "Overwrite", string(existing)
			}
			if err := hooks.Approve(ctx, writeApproval(ws, "create_file", fmt.Sprintf("%s %s (%d bytes)", verb, rel, len(input.Content)),
				unifiedDiff(rel, before, input.Content), rel)); err != nil {
				return fail(err.Error())
			}
			if input.Overwrite {
				if err := ws.unchanged(rel, existed, existing); err != nil {
					return fail(err.Error())
				}
			}

			data := []byte(input.Content)
			if input.Overwrite {
				err = ws.WriteFileAtomic(ctx, rel, data)
			} else {
				err = ws.CreateExclusive(ctx, rel, data)
				if errors.Is(err, fs.ErrExist) {
					return fail(fmt.Sprintf("file '%s' already exists; set overwrite=true to overwrite", input.Path))
				}
			}
			if err != nil {
				return fail(fmt.Sprintf("failed to write file: %v", err))
			}

			return CreateFileOutput{Path: input.Path, Success: true, BytesWritten: len(data)}, nil
		},
	)
}

// DeleteFileInput defines arguments for deleting a file.
type DeleteFileInput struct {
	Path string `json:"path" jsonschema:"Path of file to delete"`
}

// DeleteFileOutput holds result of deleting a file.
type DeleteFileOutput struct {
	Path    string `json:"path"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// NewDeleteFileTool creates an ADK tool for deleting files.
func NewDeleteFileTool(ws *Workspace, hooks *Hooks) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name:        "delete_file",
			Description: "Delete a file from the workspace",
		},
		func(ctx agent.Context, input DeleteFileInput) (DeleteFileOutput, error) {
			rel, err := ws.WritablePath(input.Path)
			if err != nil {
				return DeleteFileOutput{Path: input.Path, Error: err.Error()}, nil
			}
			unlock, err := ws.lockPaths(ctx, rel)
			if err != nil {
				return DeleteFileOutput{Path: input.Path, Error: err.Error()}, nil
			}
			defer unlock()
			var diff string
			data, readErr := ws.ReadFile(rel)
			if readErr == nil {
				diff = unifiedDiff(rel, string(data), "")
			}
			if err := hooks.Approve(ctx, api.ApprovalRequest{
				Tool:     "delete_file",
				Kind:     api.ActionDelete,
				Detail:   "Delete " + rel,
				Diff:     diff,
				Key:      "delete:" + ws.Dir(),
				KeyLabel: "file deletions in " + ws.Dir(),
				Targets:  []string{rel},
			}); err != nil {
				return DeleteFileOutput{Path: input.Path, Error: err.Error()}, nil
			}
			if readErr == nil {
				if err := ws.unchanged(rel, true, data); err != nil {
					return DeleteFileOutput{Path: input.Path, Error: err.Error()}, nil
				}
			}
			if err := ws.RemoveFile(ctx, rel); err != nil {
				return DeleteFileOutput{Path: input.Path, Error: fmt.Sprintf("failed to delete: %v", err)}, nil
			}
			return DeleteFileOutput{Path: input.Path, Success: true}, nil
		},
	)
}

// writeApproval builds an approval request for a file edit. Remembered
// approvals cover all edits within the workspace.
func writeApproval(ws *Workspace, tool, detail, diff string, paths ...string) api.ApprovalRequest {
	return api.ApprovalRequest{
		Tool:     tool,
		Kind:     api.ActionWrite,
		Detail:   detail,
		Diff:     diff,
		Key:      "write:" + ws.Dir(),
		KeyLabel: "file edits in " + ws.Dir(),
		Targets:  paths,
	}
}
