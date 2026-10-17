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

//go:build !windows

package wininstall

import "errors"

// errNotWindows: the registry is Windows'.
var errNotWindows = errors.New("the list of installed programs is on Windows only")

// WriteEntry writes the app's entry in Add or remove programs (Windows only).
func WriteEntry(Entry) error { return errNotWindows }

// DeleteEntry removes the app's entry (Windows only).
func DeleteEntry() error { return errNotWindows }

// SetDisplayVersion updates the entry's version (nothing to do but on Windows).
func SetDisplayVersion(string) error { return nil }
