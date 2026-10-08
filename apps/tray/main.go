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

// Command blitz-tray shows the Blitz service in the system tray: an icon
// with its state and, in its menu, Start, Stop and Restart, Open Blitz and
// Open the logs. It starts at login beside the service (--install), and
// talks to the service over its socket like any client.
//
// Windows has no desktop app: there the tray serves the desktop page
// itself (pkg/pageserver) and Open Blitz shows it in an Edge app window.
// The service has no login item there either, so the tray starts it, and
// its menu has Start at login for the tray.
//
// Linux needs a StatusNotifierItem host: KDE and most desktops have one;
// GNOME needs the AppIndicator extension (Ubuntu's is on by default).
// Without one there's no icon, and the tray waits quietly.
package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sync"
	"time"

	"fyne.io/systray"
	"github.com/retail-cortex/blitz/apps/tray/internal/tray"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/loginitem"
	"github.com/retail-cortex/blitz/pkg/pageserver"
	"github.com/retail-cortex/blitz/pkg/socket"
)

// version is set at link time for releases.
var version = "dev"

// every is how often the tray asks the service how it is.
const every = 2 * time.Second

func main() {
	install := flag.Bool("install", false, "Start the tray at login (and now), then exit")
	uninstall := flag.Bool("uninstall", false, "Stop starting the tray at login, then exit")
	showVersion := flag.Bool("version", false, "Print the version and exit")
	flag.Parse()
	switch {
	case *showVersion:
		fmt.Println("blitz-tray", version)
	case *install:
		exit(installTray())
	case *uninstall:
		exit(loginitem.UninstallTray())
	default:
		if !lock() {
			return // one tray is enough
		}
		systray.Run(onReady, func() {})
	}
}

func exit(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "blitz-tray:", err)
		os.Exit(1)
	}
}

// program is this program's file, links resolved, so a login entry keeps
// working when a link moves.
func program() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// installTray makes this program start at login, and starts it now.
func installTray() error {
	exe, err := program()
	if err != nil {
		return err
	}
	if err := loginitem.InstallTray(exe); err != nil {
		return err
	}
	// launchd has started it on macOS; on Linux and Windows, start it in
	// this session.
	if goruntime.GOOS == "windows" || goruntime.GOOS == "linux" && (os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "") {
		return tray.Detached(exe) // a tray already running keeps its lock; this one exits
	}
	return nil
}

// lockFile holds the lock: kept here, it isn't collected (and closed,
// releasing the lock) while the tray runs.
var lockFile *os.File

// lock holds ~/.blitz/run/tray.lock for as long as the tray runs, so a
// second tray (started by hand, or twice at login) exits.
func lock() bool {
	dir := config.ExpandHome("~/.blitz/run")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return true
	}
	f, err := os.OpenFile(filepath.Join(dir, "tray.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return true
	}
	if !tryLock(f) {
		f.Close()
		return false
	}
	lockFile = f // open, and locked, until the process ends
	return true
}

func onReady() {
	sock := socket.DefaultSocket()
	status := func() tray.Status { return tray.Probe(context.Background(), sock, loginitem.Installed) }
	acts := tray.Actions{
		Run:      tray.Detached,
		Beside:   loginitem.Beside(),
		Status:   status,
		LogDir:   func() string { return tray.ServiceLogDir(context.Background(), sock) },
		Shutdown: func() error { return tray.Shutdown(context.Background(), sock) },
		Wait: func(running bool, d time.Duration) bool {
			for end := time.Now().Add(d); ; time.Sleep(300 * time.Millisecond) {
				if socket.Running(sock) == running {
					return true
				}
				if time.Now().After(end) {
					return false
				}
			}
		},
	}

	if page != nil {
		acts.Page = pageLink(page, sock, acts)
	}

	tray.SetupLocale()
	systray.SetTitle("")
	state := systray.AddMenuItem(i18n.T("tray.service"), "")
	state.Disable()
	systray.AddSeparator()
	start := systray.AddMenuItem(i18n.T("tray.start"), "")
	stop := systray.AddMenuItem(i18n.T("tray.stop"), "")
	restart := systray.AddMenuItem(i18n.T("tray.restart"), "")
	systray.AddSeparator()
	open := systray.AddMenuItem(i18n.T("tray.open_app"), "")
	logs := systray.AddMenuItem(i18n.T("tray.open_logs"), "")
	systray.AddSeparator()
	// Windows: the tray is what starts at login (and starts the service).
	atLogin := &systray.MenuItem{ClickedCh: make(chan struct{})}
	if goruntime.GOOS == "windows" {
		atLogin = systray.AddMenuItemCheckbox(i18n.T("tray.start_at_login"), "", loginitem.TrayInstalled())
		systray.AddSeparator()
	}
	quit := systray.AddMenuItem(i18n.T("tray.quit"), i18n.T("tray.quit_hint"))

	show := func(m tray.Menu, note string) {
		setIcon(m.Running)
		title := m.State.Title
		if note != "" {
			title = note
		}
		state.SetTitle(title)
		systray.SetTooltip(title)
		for _, x := range []struct {
			mi *systray.MenuItem
			it tray.Item
		}{{start, m.Start}, {stop, m.Stop}, {restart, m.Restart}} {
			x.mi.SetTitle(x.it.Title)
			if x.it.Enabled {
				x.mi.Enable()
			} else {
				x.mi.Disable()
			}
		}
	}
	// busy is what's being done (or went wrong), shown instead of the state
	// until it's over.
	var mu sync.Mutex
	busy := ""
	setBusy := func(s string) { mu.Lock(); busy = s; mu.Unlock() }
	isBusy := func() bool { mu.Lock(); defer mu.Unlock(); return busy != "" }
	refresh := func() {
		st := status()
		mu.Lock()
		note := busy
		mu.Unlock()
		show(tray.MenuFor(st), note)
	}
	refresh()

	do := func(doing string, f func() error) {
		setBusy(doing)
		refresh()
		go func() {
			err := f()
			setBusy("")
			if err != nil {
				log.Printf("blitz-tray: %v", err)
				setBusy(i18n.T("tray.failed", "error", err.Error()))
				refresh()
				time.Sleep(5 * time.Second)
				setBusy("")
			}
			refresh()
		}()
	}
	// Windows: nothing else starts the service at login.
	if goruntime.GOOS == "windows" && !status().Running {
		do(i18n.T("tray.starting"), acts.Start)
	}
	go func() {
		for {
			select {
			case <-atLogin.ClickedCh:
				do("", func() error { return toggleAtLogin(atLogin) })
			case <-start.ClickedCh:
				do(i18n.T("tray.starting"), acts.Start)
			case <-stop.ClickedCh:
				do(i18n.T("tray.stopping"), acts.Stop)
			case <-restart.ClickedCh:
				do(i18n.T("tray.restarting"), acts.Restart)
			case <-open.ClickedCh:
				if err := acts.OpenApp(); err != nil {
					do("", func() error { return err })
				}
			case <-logs.ClickedCh:
				if err := acts.OpenLogs(); err != nil {
					do("", func() error { return err })
				}
			case <-quit.ClickedCh:
				systray.Quit()
				return
			case <-time.After(every):
				if !isBusy() {
					refresh()
				}
			}
		}
	}()
}

// toggleAtLogin starts the tray at login, or stops that, and ticks the
// menu item to match.
func toggleAtLogin(item *systray.MenuItem) error {
	var err error
	if loginitem.TrayInstalled() {
		err = loginitem.UninstallTray()
	} else if exe, e := program(); e != nil {
		err = e
	} else {
		err = loginitem.InstallTray(exe)
	}
	if loginitem.TrayInstalled() {
		item.Check()
	} else {
		item.Uncheck()
	}
	return err
}

// pageLink serves the page (on its first use) and returns a link that
// opens it, a new one each time.
func pageLink(page fs.FS, sock string, acts tray.Actions) func() (string, error) {
	var (
		mu  sync.Mutex
		srv *pageserver.Server
	)
	return func() (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if srv == nil {
			exe, _ := program()
			host := tray.Host{Actions: acts, Version: version, Socket: sock, Tray: exe}
			s, err := pageserver.Start(pageserver.Options{Page: page, Socket: sock, Host: host.Handler()})
			if err != nil {
				return "", err
			}
			srv = s
		}
		return srv.OpenURL(), nil
	}
}
