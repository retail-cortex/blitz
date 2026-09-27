package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrefsDefaultsAndRoundTrip(t *testing.T) {
	s := &prefsStore{path: filepath.Join(t.TempDir(), "blitz", "desktop.json")}
	p, err := s.load()
	if err != nil || p.Theme != "system" || p.Drawer != "open" || p.Density != "comfortable" || p.Notifications != "on" || len(p.Workspaces) != 0 {
		t.Fatalf("defaults %+v %v", p, err)
	}
	a, b := t.TempDir(), t.TempDir()
	p.Theme = "dark"
	p.Workspaces = []WorkspacePrefs{
		{Dir: a + "/", Name: "  Shop  ", Color: "teal", Open: true},
		{Dir: b, Description: "old", Open: false},
		{Dir: a, Open: true},              // the same directory again
		{Dir: "relative/dir", Open: true}, // not a directory the service can open
	}
	p.Active = b // closed: can't be active
	saved, err := s.save(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Workspaces) != 2 || saved.Workspaces[0].Dir != a || saved.Workspaces[0].Name != "Shop" || saved.Active != a {
		t.Fatalf("normalized %+v", saved)
	}
	info, err := os.Stat(s.path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode %v %v", info, err)
	}
	got, err := s.load()
	if err != nil || got.Theme != "dark" || len(got.Workspaces) != 2 || got.Workspaces[1].Description != "old" || got.Workspaces[1].Open {
		t.Fatalf("loaded %+v %v", got, err)
	}
}

// The page reads workspaces as a list: with none it must be [], not null
// (a first start crashed the page when the service came up).
func TestPrefsSendAnEmptyListNotNull(t *testing.T) {
	s := &prefsStore{path: filepath.Join(t.TempDir(), "desktop.json")}
	p, err := s.load()
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(p)
	if !strings.Contains(string(data), `"workspaces":[]`) {
		t.Fatalf("defaults encode as %s", data)
	}
	saved, _ := s.save(Prefs{})
	if data, _ := json.Marshal(saved); !strings.Contains(string(data), `"workspaces":[]`) {
		t.Fatalf("saved encode as %s", data)
	}
}

func TestPrefsKeepOpenWorkspacesFirstAndBoundRecent(t *testing.T) {
	var p Prefs
	for i := range maxRecent + 5 {
		p.Workspaces = append(p.Workspaces, WorkspacePrefs{Dir: filepath.Join("/w", string(rune('a'+i)))})
	}
	p.Workspaces = append(p.Workspaces, WorkspacePrefs{Dir: "/open", Open: true})
	p.Theme = "sepia"
	p.normalize()
	if p.Workspaces[0].Dir != "/open" || len(p.Workspaces) != maxRecent+1 || p.Theme != "system" || p.Active != "/open" {
		t.Fatalf("%d workspaces, first %s, theme %s, active %s", len(p.Workspaces), p.Workspaces[0].Dir, p.Theme, p.Active)
	}
}

func TestDamagedPrefsAreSetAside(t *testing.T) {
	dir := t.TempDir()
	s := &prefsStore{path: filepath.Join(dir, "desktop.json")}
	os.WriteFile(s.path, []byte("{nope"), 0o600)
	p, err := s.load()
	if err == nil || !strings.Contains(err.Error(), "set aside") || p.Theme != "system" {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err := os.Stat(s.path + ".damaged"); err != nil {
		t.Error("the damaged file wasn't kept")
	}
	if _, err := s.load(); err != nil {
		t.Errorf("after setting it aside: %v", err)
	}
}

func TestSafeURL(t *testing.T) {
	for link, want := range map[string]bool{
		"https://example.com/a?b":   true,
		"http://localhost:8080":     true,
		"mailto:dev@example.com":    true,
		"HTTPS://EXAMPLE.COM":       true,
		"file:///etc/passwd":        false,
		"javascript:alert(1)":       false,
		"vscode://open?x":           false,
		"https://":                  false,
		"/relative/path":            false,
		"data:text/html,<script>":   false,
		" https://example.com/ok  ": true,
	} {
		if got := safeURL(link); got != want {
			t.Errorf("%q: %v", link, got)
		}
	}
}
