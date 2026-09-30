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

package tui

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/ergochat/readline"
)

// KeyBindings are the REPL's keys (spec_parity_027 PAR-UI-09), from
// ~/.blitz/keybindings.toml:
//
//	editor = "ctrl+g"          # edit the line in $EDITOR
//	cycle_mode = "shift+tab"   # next permission mode
//	[commands]
//	"ctrl+t" = "/tasks"        # run a command at an empty prompt
//
// A key is ctrl+<letter>, shift+tab or "none". Binding a key the line
// editor uses (ctrl+a, ctrl+r, …) takes it over.
type KeyBindings struct {
	Editor    rune // 0: none
	CycleMode rune
	// Commands are sent as the entry when their key is pressed at an empty
	// prompt.
	Commands map[rune]string
}

// DefaultKeyBindings are the keys without a keybindings file.
func DefaultKeyBindings() KeyBindings {
	return KeyBindings{Editor: keyCtrlG, CycleMode: keyShiftTab}
}

// keyFile is the keybindings file's format.
type keyFile struct {
	Editor    *string           `toml:"editor"`
	CycleMode *string           `toml:"cycle_mode"`
	Commands  map[string]string `toml:"commands"`
}

// LoadKeyBindings reads path over the defaults; a missing file is the
// defaults.
func LoadKeyBindings(path string) (KeyBindings, error) {
	k := DefaultKeyBindings()
	if path == "" {
		return k, nil
	}
	var f keyFile
	md, err := toml.DecodeFile(path, &f)
	if errors.Is(err, os.ErrNotExist) {
		return k, nil
	}
	if err != nil {
		return k, fmt.Errorf("%s: %w", path, err)
	}
	if un := md.Undecoded(); len(un) > 0 {
		return k, fmt.Errorf("%s: unknown key %q", path, un[0].String())
	}
	for _, b := range []struct {
		set  *string
		to   *rune
		name string
	}{{f.Editor, &k.Editor, "editor"}, {f.CycleMode, &k.CycleMode, "cycle_mode"}} {
		if b.set == nil {
			continue
		}
		r, err := parseKey(*b.set)
		if err != nil {
			return k, fmt.Errorf("%s: %s: %w", path, b.name, err)
		}
		*b.to = r
	}
	names := make([]string, 0, len(f.Commands))
	for name := range f.Commands {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		r, err := parseKey(name)
		if err != nil || r == 0 {
			return k, fmt.Errorf("%s: commands: %q: %w", path, name, errors.Join(err, errors.New("not a key")))
		}
		cmd := strings.TrimSpace(f.Commands[name])
		if !strings.HasPrefix(cmd, "/") || strings.ContainsAny(cmd, "\r\n") {
			return k, fmt.Errorf("%s: commands: %q: %q isn't a /command", path, name, cmd)
		}
		if r == k.Editor || r == k.CycleMode {
			return k, fmt.Errorf("%s: commands: %q is already bound", path, name)
		}
		if k.Commands == nil {
			k.Commands = map[rune]string{}
		}
		k.Commands[r] = cmd
	}
	if k.Editor != 0 && k.Editor == k.CycleMode {
		return k, fmt.Errorf("%s: editor and cycle_mode are the same key", path)
	}
	return k, nil
}

// parseKey reads ctrl+<letter>, shift+tab or none (0).
func parseKey(s string) (rune, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "none", "":
		return 0, nil
	case "shift+tab":
		return readline.MetaShiftTab, nil
	}
	if l, ok := strings.CutPrefix(s, "ctrl+"); ok && len(l) == 1 && l[0] >= 'a' && l[0] <= 'z' {
		switch l[0] {
		case 'c', 'd', 'm', 'j', 'i':
			// Interrupt, end of input, Enter and Tab stay theirs.
			return 0, fmt.Errorf("%s can't be rebound", s)
		}
		return rune(l[0]-'a') + 1, nil
	}
	return 0, fmt.Errorf("%q isn't a key (ctrl+<letter>, shift+tab or none)", s)
}
