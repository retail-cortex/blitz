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

//go:build windows || traypage

package main

import (
	"embed"
	"io/fs"
)

// The desktop page, which the tray serves where there's no desktop app
// (Windows; the traypage tag builds it elsewhere, to try it).
//
//go:embed all:dist
var dist embed.FS

// page is the page's files, index.html at the root.
var page, _ = fs.Sub(dist, "dist")
