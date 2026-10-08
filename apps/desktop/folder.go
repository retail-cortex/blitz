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

package main

import "github.com/retail-cortex/blitz/pkg/fileview"

// OpenFolder shows a folder (the logs', for one) in the file manager. Only
// an existing directory opens: the page can't make it run a file.
func (a *App) OpenFolder(dir string) error { return fileview.OpenFolder(dir) }

// OpenDocument shows an image, PDF, sound or video in the system's viewer
// (where the window's own preview falls short). Other files are refused.
func (a *App) OpenDocument(path string) error { return fileview.OpenDocument(path) }

// RevealPath shows a file or folder selected in the system's file manager.
func (a *App) RevealPath(path string) error { return fileview.RevealPath(path) }

// FileManager is the name of the system's file manager, for "Show in …"
// ("" when it has none we know by name).
func (a *App) FileManager() string { return fileview.FileManager() }
