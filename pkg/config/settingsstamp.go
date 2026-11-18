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

package config

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

// GlobalSettingsFiles are the files the global settings load from (Load):
// the person's .env.toml in the settings directory (prefixDir as Load
// takes it) and device management's machine-wide settings.
func GlobalSettingsFiles(prefixDir string) []string {
	var files []string
	if dir := ConfigDir(prefixDir); dir != "" {
		files = append(files, filepath.Join(dir, ".env.toml"))
	}
	return append(files, ManagedSettingsPath())
}

// SettingsStamp is files as they are now, to tell later whether any
// changed (spec_021_service SVC-06b): each one's path and the SHA-256 of
// its content, or nothing for one that isn't there.
func SettingsStamp(files []string) string {
	var b strings.Builder
	for _, f := range files {
		b.WriteString(f)
		b.WriteByte(0)
		if data, err := os.ReadFile(f); err == nil {
			sum := sha256.Sum256(data)
			b.WriteString(hex.EncodeToString(sum[:]))
		}
		b.WriteByte(0)
	}
	return b.String()
}
