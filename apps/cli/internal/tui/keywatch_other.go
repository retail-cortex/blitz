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

//go:build !darwin && !linux

package tui

import (
	"errors"
	"time"
)

var errKeysUnsupported = errors.New("key watching is not supported on this platform")

// Steering needs raw key input; elsewhere the watcher exits at once and
// turns behave as before.
type ttyKeys struct{}

func newTTYKeys(int) keyTerm                      { return ttyKeys{} }
func newPickerKeys(int) keyTerm                   { return ttyKeys{} }
func (ttyKeys) enter() error                      { return errKeysUnsupported }
func (ttyKeys) leave() error                      { return nil }
func (ttyKeys) ready(time.Duration) (bool, error) { return false, errKeysUnsupported }
func (ttyKeys) read([]byte) (int, error)          { return 0, errKeysUnsupported }
