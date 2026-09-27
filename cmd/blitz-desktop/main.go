// Command blitz-desktop is Blitz's desktop app: a window with a
// tab per workspace, driving the per-user Blitz service.
package main

import (
	"embed"
	"log"

	"github.com/retail-cortex/blitz/internal/config"
	"github.com/retail-cortex/blitz/internal/server"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

// dist is the page, built from web/desktop.
//
//go:embed all:dist
var dist embed.FS

func main() {
	app := &App{socket: server.DefaultSocket(), prefs: &prefsStore{path: config.ExpandHome("~/.blitz/desktop.json")}}
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
		Bind:      []any{app},
	})
	if err != nil {
		log.Fatal(err)
	}
}
