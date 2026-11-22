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

// Package docs carries the user docs in the programs, so help works offline
// and matches the release (spec_067_cli_parity CLP-21): the home page,
// getting started, tutorials, guide, products and releases, as the desktop
// app carries them (spec_024_desktop DSK-103). The pictures stay on the site.
package docs

import "embed"

// User is the user docs' Markdown, at its path under content/
// (content/guide/mcp.md).
//
//go:embed content/_index.md all:content/getting-started all:content/tutorials all:content/guide all:content/products all:content/releases
var User embed.FS
