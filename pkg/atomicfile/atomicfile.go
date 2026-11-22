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

// Package atomicfile replaces a file whole: readers see the old content
// or the new, never part of it, even after a crash part way through.
package atomicfile

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
)

// Write replaces the file at path with data, made with perm: written to
// a temporary file beside it, flushed to disk, then renamed over it.
func Write(path string, data []byte, perm fs.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // gone already once renamed
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	for _, step := range []func() error{func() error { return tmp.Chmod(perm) }, tmp.Sync} {
		if err := step(); err != nil {
			tmp.Close()
			return err
		}
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// MkdirWrite ensures path's parent directory exists with dirPerm, then
// atomically writes data to path with filePerm via Write.
func MkdirWrite(path string, data []byte, dirPerm, filePerm fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return err
	}
	return Write(path, data, filePerm)
}

// WriteJSON marshals v as indented JSON with a trailing newline and writes
// it atomically via MkdirWrite (creating missing parent directories with
// mode 0700 and the file with perm).
func WriteJSON(path string, v any, perm fs.FileMode) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return MkdirWrite(path, append(data, '\n'), 0o700, perm)
}

// ReadJSONOrQuarantine reads JSON from path into dst and reports whether a
// file was decoded. Missing files return (false, nil). Damaged JSON is logged,
// moved aside to path+".damaged", and reported as an error.
func ReadJSONOrQuarantine(path string, dst any) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("reading %s: %w", filepath.Base(path), err)
	}
	if err := json.Unmarshal(data, dst); err != nil {
		slog.Warn("quarantining damaged JSON state file", "path", path, "error", err)
		out := fmt.Errorf("damaged JSON in %s (set aside): %w", filepath.Base(path), err)
		if rerr := os.Rename(path, path+".damaged"); rerr != nil {
			return false, errors.Join(out, rerr)
		}
		return false, out
	}
	return true, nil
}
