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
// --serve serves the page without an icon, for the Electron app's window
// (apps/electron), which starts it: it prints a link to the page and
// stops when its standard input closes.
//
// Linux needs a StatusNotifierItem host: KDE and most desktops have one;
// GNOME needs the AppIndicator extension (Ubuntu's is on by default).
// Without one there's no icon, and the tray waits quietly.
package main

import (
	"context"
	"errors"
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
	"github.com/retail-cortex/blitz/pkg/pagehost"
	"github.com/retail-cortex/blitz/pkg/pageserver"
	"github.com/retail-cortex/blitz/pkg/selfupdate"
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
	serve := flag.Bool("serve", false, "Serve the desktop page for a window (the Electron app): print a link to it as a JSON line, a new one for each \"link\" line read, until standard input closes")
	pageDir := flag.String("page", "", "With --serve: the built page's directory (default: the page built into the tray)")
	applyUpdate := flag.Bool("apply-update", false, "Put a downloaded desktop app in place of the running one (the app's host runs this), then start it")
	staged := flag.String("staged", "", "With --apply-update: the new app")
	root := flag.String("root", "", "With --apply-update: the app it replaces")
	relaunch := flag.String("relaunch", "", "With --apply-update: what starts the app again")
	waitPID := flag.Int("wait-pid", 0, "With --apply-update: the app's process, waited for")
	flag.Parse()
	switch {
	case *showVersion:
		fmt.Println("blitz-tray", version)
	case *serve:
		exit(serveWindow(*pageDir))
	case *applyUpdate:
		exit(applyDownloaded(*staged, *root, *relaunch, *waitPID))
	case *install:
		exit(installTray())
	case *uninstall:
		exit(loginitem.UninstallTray())
	default:
		if !lock() {
			return // one tray is enough
		}
		menuBarOnly()
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
	if goruntime.GOOS == "darwin" {
		return nil
	}
	return startTray(exe)
}

// startTray starts exe (this program) as the tray in this session, on
// Windows or a Linux desktop (none without a display). A tray already
// running keeps its lock; the new one exits.
func startTray(exe string) error {
	if goruntime.GOOS == "linux" && os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return nil
	}
	return tray.Detached(exe)
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

// serveWindow serves the page for the Electron app's window (--serve):
// the page in dir, else the one built in; no icon, and no tray lock, so a
// tray can show its icon beside it.
func serveWindow(dir string) error {
	p := page
	if dir != "" {
		d, err := tray.PageDir(dir)
		if err != nil {
			return err
		}
		p = d
	}
	if p == nil {
		return errors.New("this blitz-tray has no page built in: pass --page")
	}
	sock := socket.DefaultSocket()
	host := newHost(sock, newActions(sock))
	// Login entries an older install wrote may name programs since moved.
	go func() {
		if err := host.Repair(); err != nil {
			fmt.Fprintln(os.Stderr, "blitz-tray:", err)
		}
	}()
	// The Electron app updates itself through its host (pkg/pagehost).
	host.Updates = &pagehost.Updater{
		Version: version, HostExe: host.Tray,
		Dir: config.ExpandHome("~/.blitz/updates"), CacheDir: config.ExpandHome("~/.blitz"),
		Source: selfupdate.GitHub, Spawn: tray.Detached, GOOS: goruntime.GOOS, GOARCH: goruntime.GOARCH,
	}
	return tray.Serve(pageserver.Options{Page: p, Socket: sock, Host: host.Handler()}, os.Stdin, os.Stdout)
}

// applyDownloaded puts the staged app in place of root (--apply-update),
// logging to ~/.blitz/logs/update.log.
func applyDownloaded(staged, root, relaunch string, waitPID int) error {
	dir := config.ExpandHome("~/.blitz/logs")
	_ = os.MkdirAll(dir, 0o700)
	log, err := os.OpenFile(filepath.Join(dir, "update.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer log.Close()
	sock := socket.DefaultSocket()
	acts := newActions(sock)
	// The service starts again from the new app, at the same path.
	acts.Beside = filepath.Join(root, "resources", "app", "bin")
	if goruntime.GOOS == "darwin" {
		acts.Beside = filepath.Join(root, "Contents", "Resources", "app", "bin")
	}
	err = tray.Update{
		Staged: staged, Root: root, Relaunch: relaunch, WaitPID: waitPID, Actions: acts,
		ServiceExe: func() string { return tray.ServiceExecutable(context.Background(), sock) },
		Exited:     tray.Exited,
		Log:        log,
	}.Run()
	if err != nil {
		fmt.Fprintln(log, time.Now().Format(time.RFC3339), err)
	}
	return err
}

// newActions controls the service on sock.
func newActions(sock string) tray.Actions {
	status := func() tray.Status { return tray.Probe(context.Background(), sock, loginitem.Installed) }
	return tray.Actions{
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
}

// newHost answers the page's calls to its host, with acts for the
// service.
func newHost(sock string, acts tray.Actions) pagehost.Host {
	exe, _ := program()
	return pagehost.Host{Service: acts, Beside: acts.Beside, Version: version, Socket: sock, Tray: exe, StartTray: func() error { return startTray(exe) }, Prefs: pagehost.NewPrefsStore(config.ExpandHome("~/.blitz/desktop.json"))}
}

func onReady() {
	sock := socket.DefaultSocket()
	acts := newActions(sock)
	status := acts.Status

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
			s, err := pageserver.Start(pageserver.Options{Page: page, Socket: sock, Host: newHost(sock, acts).Handler()})
			if err != nil {
				return "", err
			}
			srv = s
		}
		return srv.OpenURL(), nil
	}
}
