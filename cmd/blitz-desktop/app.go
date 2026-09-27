package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/internal/config"
	"github.com/retail-cortex/blitz/internal/server"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is what the page can call in Go (bound by Wails): what the service
// can't do itself, namely report whether it runs, install it, and native
// dialogs.
type App struct {
	ctx    context.Context
	socket string
	prefs  *prefsStore

	notifyOnce sync.Once
	notifyOK   bool // notifications work and the user allowed them
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	// A click on a notification brings the window back, on its workspace.
	if runtime.InitializeNotifications(ctx) == nil {
		runtime.OnNotificationResponse(ctx, func(r runtime.NotificationResult) {
			if r.Error != nil {
				return
			}
			runtime.WindowUnminimise(ctx)
			runtime.WindowShow(ctx)
			if dir, ok := r.Response.UserInfo["dir"].(string); ok {
				runtime.EventsEmit(ctx, "notification:open", dir)
			}
		})
	}
}

// Notify shows a system notification (the agent finished, or waits for
// the user). The first one asks the user's permission; without it, or
// where notifications don't work, it does nothing. dir is the workspace a
// click opens.
func (a *App) Notify(title, body, dir string) error {
	a.notifyOnce.Do(func() {
		if !runtime.IsNotificationAvailable(a.ctx) {
			return
		}
		ok, err := runtime.RequestNotificationAuthorization(a.ctx)
		a.notifyOK = ok && err == nil
	})
	if !a.notifyOK {
		return nil
	}
	return runtime.SendNotification(a.ctx, runtime.NotificationOptions{
		ID:    fmt.Sprintf("blitz-%d", time.Now().UnixNano()),
		Title: title,
		Body:  body,
		Data:  map[string]any{"dir": dir},
	})
}

// ServiceStatus says whether the service answers and whether it starts at
// login.
type ServiceStatus struct {
	Running   bool   `json:"running"`
	Installed bool   `json:"installed"`
	Socket    string `json:"socket"`
	// CLI is the blitz binary that installs the service ("" if none).
	CLI string `json:"cli"`
}

func (a *App) ServiceStatus() ServiceStatus {
	cli, _ := findCLI()
	return ServiceStatus{Running: server.Running(a.socket), Installed: loginItemInstalled(), Socket: a.socket, CLI: cli}
}

// InstallService runs `blitz service install`, which starts the
// service now and at every login.
func (a *App) InstallService() (string, error) {
	cli, err := findCLI()
	if err != nil {
		return "", err
	}
	out, err := exec.CommandContext(a.ctx, cli, "service", "install").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, out)
	}
	return string(out), nil
}

// ChooseWorkspace asks for a directory to open ("" if cancelled).
func (a *App) ChooseWorkspace() (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Open a workspace", CanCreateDirectories: true})
}

// GetPrefs returns the window's settings (the defaults when none are
// saved). A damaged file is reported once and replaced by the defaults.
func (a *App) GetPrefs() (Prefs, error) { return a.prefs.load() }

// SavePrefs saves the window's settings and returns them as saved
// (normalized).
func (a *App) SavePrefs(p Prefs) (Prefs, error) { return a.prefs.save(p) }

// OpenURL opens a web or mail link in the system browser, never in the
// window. Other schemes are refused.
func (a *App) OpenURL(link string) error {
	if !safeURL(link) {
		return fmt.Errorf("not opening %q: only web and mail links open", link)
	}
	runtime.BrowserOpenURL(a.ctx, link)
	return nil
}

// findCLI finds the blitz command: next to this app's binary (as
// bundled), else on PATH.
func findCLI() (string, error) {
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), "blitz")
		if goruntime.GOOS == "windows" {
			p += ".exe"
		}
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	if p, err := exec.LookPath("blitz"); err == nil {
		return p, nil
	}
	return "", errors.New("the blitz command isn't installed")
}

// loginItemInstalled reports whether `blitz service install` has run.
func loginItemInstalled() bool {
	var p string
	switch goruntime.GOOS {
	case "darwin":
		p = "~/Library/LaunchAgents/dev.blitz.service.plist"
	case "linux":
		p = "~/.config/systemd/user/blitz.service"
	default:
		return false
	}
	_, err := os.Stat(config.ExpandHome(p))
	return err == nil
}
