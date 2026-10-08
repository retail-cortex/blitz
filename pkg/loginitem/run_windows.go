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

package loginitem

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

// runKey is where Windows keeps this user's programs to start at login.
const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// runValue is the tray's value in runKey.
const runValue = "Blitz"

// runPath names the tray's start-at-login entry: a value in the registry.
func runPath() string { return `HKCU\` + runKey + `\` + runValue }

// runInstalled reports whether the tray's value is in runKey.
func runInstalled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(runValue)
	return err == nil
}

// installRun starts bin at login: its value in runKey, quoted, as paths
// with spaces need.
func installRun(bin string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(runValue, `"`+bin+`"`)
}

// uninstallRun removes the tray's value (none is fine).
func uninstallRun() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer k.Close()
	if err := k.DeleteValue(runValue); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}
