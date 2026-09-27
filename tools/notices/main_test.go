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
	"path/filepath"
	"strings"
	"testing"
)

func TestIdentify(t *testing.T) {
	for text, want := range map[string]string{
		"Apache License\n Version 2.0, January 2004":                                       "Apache-2.0",
		"Permission is hereby granted, free of charge, to any person":                      "MIT",
		"Redistribution and use in source and binary forms ... Neither the name of Google": "BSD-3-Clause",
		"Redistribution and use in source and binary forms, with or without":               "BSD-2-Clause",
		"Permission to use, copy, modify, and/or distribute this software for any purpose": "ISC",
		"Mozilla Public License Version 2.0":                                               "MPL-2.0",
		"GNU GENERAL PUBLIC LICENSE Version 3":                                             "",
		"All rights reserved.":                                                             "",
	} {
		if got := identify(text); got != want {
			t.Errorf("identify(%q) = %q, want %q", text[:20], got, want)
		}
	}
}

func TestNames(t *testing.T) {
	if got := repoName("github.com/pkg/errors"); got != "com_github_pkg_errors" {
		t.Errorf("repoName: %s", got)
	}
	if got := repoName("gopkg.in/yaml.v3"); got != "in_gopkg_yaml_v3" {
		t.Errorf("repoName: %s", got)
	}
	if got := storeDir("react-dom@19.3.0(react@19.3.0)"); got != "react-dom@19.3.0_react@19.3.0" {
		t.Errorf("storeDir: %s", got)
	}
	if n, v := splitNpmKey("@codemirror/view@6.43.13"); n != "@codemirror/view" || v != "6.43.13" {
		t.Errorf("splitNpmKey: %s %s", n, v)
	}
}

// A component with no license file, or an unaccepted one, is refused;
// an npm package without one falls back to its package.json.
func TestReadComponent(t *testing.T) {
	dir := t.TempDir()
	write := func(name, text string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := readComponent("x", "1", dir); err == nil || !strings.Contains(err.Error(), "no license file") {
		t.Errorf("no file: %v", err)
	}
	write("package.json", `{"license": "MIT", "author": {"name": "Ada"}}`)
	c, err := readNpm("x", "1", dir)
	if err != nil || c.license != "MIT" || !strings.Contains(c.texts["package.json"], "Author: Ada") {
		t.Errorf("package.json fallback: %+v %v", c, err)
	}
	write("package.json", `{"license": "GPL-3.0"}`)
	if _, err := readNpm("x", "1", dir); err == nil {
		t.Error("GPL accepted")
	}
	write("LICENSE", "GNU GENERAL PUBLIC LICENSE Version 3")
	if _, err := readComponent("x", "1", dir); err == nil {
		t.Error("an unaccepted license file accepted")
	}
}
