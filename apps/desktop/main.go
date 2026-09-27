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

// Command blitz-desktop is Blitz's desktop app: a window with a
// tab per workspace, driving the per-user Blitz service.
package main

import (
	"embed"
	"log"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/socket"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

// dist is the page, built from web/.
//
//go:embed all:dist
var dist embed.FS

// version is set when a release is built (the git tag).
var version = "dev"

func main() {
	app := &App{socket: socket.DefaultSocket(), prefs: &prefsStore{path: config.ExpandHome("~/.blitz/desktop.json")}}
	err := wails.Run(&options.App{
		Title:     "Blitz",
		Width:     1280,
		Height:    820,
		MinWidth:  720,
		MinHeight: 480,
		// The page draws the whole window, title bar area included, in the
		// theme's own colours (it may differ from the system's).
		Mac: &mac.Options{
			TitleBar:             mac.TitleBarHiddenInset(),
			WebviewIsTransparent: false,
		},
		AssetServer: &assetserver.Options{
			Assets:  dist,
			Handler: serviceProxy(app.socket),
		},
		OnStartup: app.startup,
		// A window with unsaved changes in the editor asks first.
		OnBeforeClose: app.beforeClose,
		Bind:          []any{app},
	})
	if err != nil {
		log.Fatal(err)
	}
}
