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

// Package jsonl appends records to the JSON Lines files Blitz keeps
// (sessions' transcripts and events, jobs' runs) so that a crash part way
// through a write costs only the record it tore.
package jsonl

import (
	"io"
	"io/fs"
	"os"
)

// Append writes line and a newline at the end of the file at path,
// making it with perm. When the file's last line was torn (a crash left
// it without its newline), a newline goes first: the record starts a
// line of its own, so readers that skip a bad line skip only the torn
// one, not this one too.
func Append(path string, perm fs.FileMode, line []byte) error {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, perm)
	if err != nil {
		return err
	}
	buf := make([]byte, 0, len(line)+2)
	if torn, err := tornTail(f); err != nil {
		f.Close()
		return err
	} else if torn {
		buf = append(buf, '\n')
	}
	buf = append(append(buf, line...), '\n')
	if _, err := f.Write(buf); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// tornTail reports whether f is non-empty and doesn't end in a newline.
func tornTail(f *os.File) (bool, error) {
	info, err := f.Stat()
	if err != nil || info.Size() == 0 {
		return false, err
	}
	last := make([]byte, 1)
	if _, err := f.ReadAt(last, info.Size()-1); err != nil && err != io.EOF {
		return false, err
	}
	return last[0] != '\n', nil
}
