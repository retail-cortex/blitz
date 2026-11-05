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
	"io/fs"
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
