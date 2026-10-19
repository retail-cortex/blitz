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

package main

import (
	"errors"

	"github.com/retail-cortex/blitz/pkg/wininstall"
)

// otherSystem stands in elsewhere: the setup program is Windows'.
type otherSystem struct{}

func newSystem() system { return otherSystem{} }

var errWindowsOnly = errors.New("the setup program runs on Windows only")

func (otherSystem) Self() (string, error)             { return "", errWindowsOnly }
func (otherSystem) LocalAppData() string              { return "" }
func (otherSystem) TempDir() string                   { return "" }
func (otherSystem) StopApp(string) error              { return errWindowsOnly }
func (otherSystem) StartMenu() string                 { return "" }
func (otherSystem) Desktop() string                   { return "" }
func (otherSystem) Shortcut(string, string) error     { return errWindowsOnly }
func (otherSystem) WriteEntry(wininstall.Entry) error { return errWindowsOnly }
func (otherSystem) DeleteEntry() error                { return errWindowsOnly }
func (otherSystem) RemoveRunValue() error             { return errWindowsOnly }
func (otherSystem) RemoveFromPath(string) error       { return errWindowsOnly }
func (otherSystem) Start(string, ...string) error     { return errWindowsOnly }
func (otherSystem) Ask(string) bool                   { return false }
func (otherSystem) Tell(string, bool)                 {}
func (otherSystem) DeleteLater(string)                {}
