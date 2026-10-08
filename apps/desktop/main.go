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

// Command blitz-desktop is Blitz's desktop app: a window laid out like an
// IDE (the workspace's files, an editor and the agent's chat), driving the
// per-user Blitz service (spec_desktop_024).
package main

import (
	"embed"
	"log"
	"os"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/pageserver"
	"github.com/retail-cortex/blitz/pkg/socket"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// dist is the page, built from web/.
//
//go:embed all:dist
var dist embed.FS

// version is set when a release is built (the git tag).
var version = "dev"

// maxWindowSize is the window's largest width and height: more than any
// screen (8K is 7680 wide).
const maxWindowSize = 16384

func main() {
	app := &App{socket: socket.DefaultSocket(), prefs: &prefsStore{path: config.ExpandHome("~/.blitz/desktop.json")}}
	app.links.emit = showLink
	for _, l := range linkArgs(os.Args[1:]) { // Linux: opened with a link
		app.links.receive(l)
	}
	err := wails.Run(&options.App{
		Title:     "Blitz",
		Width:     1280,
		Height:    820,
		MinWidth:  720,
		MinHeight: 480,
		// No maximum, in effect. Left at 0, Wails caps a Linux window at
		// the monitor it guesses it's on as it opens, which on Wayland
		// (where GDK doesn't know) can be another one: maximised on a
		// larger second screen, the window stopped at the laptop's size.
		MaxWidth:  maxWindowSize,
		MaxHeight: maxWindowSize,
		// The page draws the whole window, title bar area included. The
		// window is translucent (the desktop shows through, blurred) and
		// the page's glass surfaces sit on it; with transparency off in
		// the app's settings the page draws solid surfaces over it.
		BackgroundColour: &options.RGBA{R: 0, G: 0, B: 0, A: 0},
		Mac: &mac.Options{
			TitleBar:             mac.TitleBarHiddenInset(),
			WebviewIsTransparent: true,
			WindowIsTranslucent:  true,
			OnUrlOpen:            app.links.receive,
		},
		// One app: a second start (a link, on Linux) goes to the first.
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "dev.blitz.desktop",
			OnSecondInstanceLaunch: func(d options.SecondInstanceData) {
				for _, l := range linkArgs(d.Args) {
					app.links.receive(l)
				}
				if app.ctx != nil {
					runtime.WindowUnminimise(app.ctx)
					runtime.WindowShow(app.ctx)
				}
			},
		},
		AssetServer: &assetserver.Options{
			Assets:  dist,
			Handler: pageserver.Proxy(app.socket),
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
