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

import (
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/retail-cortex/blitz/pkg/loginitem"
	"github.com/retail-cortex/blitz/pkg/pagehost"
	"github.com/retail-cortex/blitz/pkg/wininstall"
	"golang.org/x/sys/windows"
)

// windowsSystem is Windows itself.
type windowsSystem struct{}

func newSystem() system { return windowsSystem{} }

func (windowsSystem) Self() (string, error) { return os.Executable() }
func (windowsSystem) LocalAppData() string  { return os.Getenv("LOCALAPPDATA") }
func (windowsSystem) TempDir() string       { return os.TempDir() }

// quote is s as a PowerShell string in single quotes.
func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// powershell runs a PowerShell command, without a window.
func powershell(command string) error {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	if out, err := cmd.CombinedOutput(); err != nil {
		return &psError{err: err, out: strings.TrimSpace(string(out))}
	}
	return nil
}

type psError struct {
	err error
	out string
}

func (e *psError) Error() string { return e.err.Error() + ": " + e.out }

func (windowsSystem) StopApp(root string) error {
	return powershell("Get-Process | Where-Object { $_.Path -and $_.Path.StartsWith(" + quote(root+`\`) + ", [StringComparison]::OrdinalIgnoreCase) } | Stop-Process -Force -ErrorAction SilentlyContinue")
}

func knownFolder(id *windows.KNOWNFOLDERID) string {
	p, err := windows.KnownFolderPath(id, 0)
	if err != nil {
		return ""
	}
	return p
}

func (windowsSystem) StartMenu() string { return knownFolder(windows.FOLDERID_Programs) }
func (windowsSystem) Desktop() string   { return knownFolder(windows.FOLDERID_Desktop) }

func (windowsSystem) Shortcut(link, target string) error {
	dir := target[:strings.LastIndex(target, `\`)]
	return powershell("$s = (New-Object -ComObject WScript.Shell).CreateShortcut(" + quote(link) + "); $s.TargetPath = " + quote(target) + "; $s.WorkingDirectory = " + quote(dir) + "; $s.Save()")
}

func (windowsSystem) WriteEntry(e wininstall.Entry) error { return wininstall.WriteEntry(e) }
func (windowsSystem) DeleteEntry() error                  { return wininstall.DeleteEntry() }
func (windowsSystem) RemoveRunValue() error               { return loginitem.UninstallTray() }
func (windowsSystem) RemoveFromPath(dir string) error     { return pagehost.RemoveFromUserPath(dir) }

func (windowsSystem) Start(exe string, args ...string) error {
	return exec.Command(exe, args...).Start()
}

func messageBox(text string, flags uint32) int32 {
	t, _ := windows.UTF16PtrFromString(text)
	c, _ := windows.UTF16PtrFromString("Blitz Setup")
	r, _ := windows.MessageBox(0, t, c, flags)
	return r
}

func (windowsSystem) Ask(text string) bool {
	const idYes = 6 // MessageBox's answer for Yes
	return messageBox(text, windows.MB_YESNO|windows.MB_ICONQUESTION) == idYes
}

func (windowsSystem) Tell(text string, failed bool) {
	icon := uint32(windows.MB_ICONINFORMATION)
	if failed {
		icon = windows.MB_ICONERROR
	}
	messageBox(text, windows.MB_OK|icon)
}

// DeleteLater asks cmd to delete path a couple of seconds from now, once
// this program has exited and let go of it.
func (windowsSystem) DeleteLater(path string) {
	cmd := exec.Command("cmd.exe", "/c", `ping -n 3 127.0.0.1 >nul & del "`+path+`"`)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW | windows.DETACHED_PROCESS}
	_ = cmd.Start()
}
