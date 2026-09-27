package loginitem

import (
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
)

// record replaces RunSystem for a test and returns what ran.
func record(t *testing.T) *[]string {
	var ran []string
	old := RunSystem
	RunSystem = func(name string, args ...string) error {
		ran = append(ran, name+" "+strings.Join(args, " "))
		return nil
	}
	t.Cleanup(func() { RunSystem = old })
	return &ran
}

func TestInstallStopUninstall(t *testing.T) {
	if goruntime.GOOS != "darwin" && goruntime.GOOS != "linux" {
		t.Skip("login items are only supported on macOS and Linux")
	}
	t.Setenv("HOME", t.TempDir())
	ran := record(t)
	bin := "/opt/blitz & co/blitzd" // escaped in the plist, quoted in the unit
	if err := Install(bin); err != nil {
		t.Fatal(err)
	}
	path, _ := Path()
	data, err := os.ReadFile(path)
	if err != nil || !Installed() {
		t.Fatalf("login item not written: %v", err)
	}
	switch goruntime.GOOS {
	case "darwin":
		dec := xml.NewDecoder(strings.NewReader(string(data)))
		for {
			if _, err := dec.Token(); err == io.EOF {
				break
			} else if err != nil {
				t.Fatalf("plist isn't well-formed: %v", err)
			}
		}
		if !strings.Contains(string(data), "/opt/blitz &amp; co/blitzd") {
			t.Errorf("plist doesn't run blitzd:\n%s", data)
		}
		if len(*ran) != 2 || !strings.HasPrefix((*ran)[0], "launchctl bootout gui/") || !strings.HasPrefix((*ran)[1], "launchctl bootstrap gui/") {
			t.Errorf("install ran %q", *ran)
		}
	case "linux":
		if !strings.Contains(string(data), `ExecStart="/opt/blitz & co/blitzd"`) {
			t.Errorf("unit doesn't run blitzd:\n%s", data)
		}
		// A running unit restarts, so a reinstall runs the new program.
		if strings.Join(*ran, "; ") != "systemctl --user daemon-reload; systemctl --user enable blitz.service; systemctl --user restart blitz.service" {
			t.Errorf("install ran %q", *ran)
		}
	}

	*ran = nil
	if err := Stop(); err != nil || len(*ran) != 1 {
		t.Errorf("stop: %v, ran %q", err, *ran)
	}
	if !Installed() {
		t.Error("stop removed the login item")
	}
	if err := Uninstall(); err != nil || Installed() {
		t.Errorf("uninstall: %v, installed %v", err, Installed())
	}
}

func TestFindService(t *testing.T) {
	beside, onPath := t.TempDir(), t.TempDir()
	for _, d := range []string{beside, onPath} {
		if err := os.WriteFile(filepath.Join(d, "blitzd"), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", onPath)
	want := func(d string) string { p, _ := filepath.EvalSymlinks(filepath.Join(d, "blitzd")); return p }
	if got, err := FindService(t.TempDir(), beside); err != nil || got != want(beside) {
		t.Errorf("beside: %q, %v", got, err)
	}
	if got, err := FindService(t.TempDir()); err != nil || got != want(onPath) {
		t.Errorf("on PATH: %q, %v", got, err)
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := FindService(); err == nil {
		t.Error("found a blitzd that isn't there")
	}
}
