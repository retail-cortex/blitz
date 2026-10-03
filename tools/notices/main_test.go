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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIdentify(t *testing.T) {
	for text, want := range map[string]string{
		"Apache License\n Version 2.0, January 2004":                                       "Apache-2.0",
		"Permission is hereby granted, free of charge, to any person":                      "MIT",
		"Redistribution and use in source and binary forms ... Neither the name of Google": "BSD-3-Clause",
		"Redistribution and use in source and binary forms, with or without":               "BSD-2-Clause",
		"Permission to use, copy, modify, and/or distribute this software for any purpose": "ISC",
		"This is free and unencumbered software released into the public domain.":          "Unlicense",
		"Mozilla Public License Version 2.0":                                               "MPL-2.0",
		"This Font Software is licensed under the SIL Open Font License, Version 1.1.":     "OFL-1.1",
		"GNU GENERAL PUBLIC LICENSE Version 3":                                             "",
		"All rights reserved.":                                                             "",
	} {
		t.Run(text, func(t *testing.T) {
			got := identify(text)
			assert.Equal(t, want, got, "identify(%q) = %q, want %q", text[:20], got, want)
		})
	}
}

func TestNames(t *testing.T) {
	got := repoName("github.com/pkg/errors")
	assert.Equal(t, "com_github_pkg_errors", got, "repoName: %s", got)
	got = repoName("gopkg.in/yaml.v3")
	assert.Equal(t, "in_gopkg_yaml_v3", got, "repoName: %s", got)
	got = storeDir("react-dom@19.3.0(react@19.3.0)")
	assert.Equal(t, "react-dom@19.3.0_react@19.3.0", got, "storeDir: %s", got)
	n, v := splitNpmKey("@codemirror/view@6.43.13")
	assert.Equal(t, "@codemirror/view", n, "splitNpmKey: %s %s", n, v)
	assert.Equal(t, "6.43.13", v, "splitNpmKey: %s %s", n, v)
}

// A component with no license file, or an unaccepted one, is refused;
// an npm package without one falls back to its package.json.
func TestReadComponent(t *testing.T) {
	dir := t.TempDir()
	write := func(name, text string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644))
	}
	_, err := readComponent("x", "1", dir)
	assert.Error(t, err, "no file")
	assert.Contains(t, err.Error(), "no license file", "no file: %v", err)
	write("package.json", `{"license": "MIT", "author": {"name": "Ada"}}`)
	c, err := readNpm("x", "1", dir)
	assert.NoError(t, err, "package.json fallback: %+v", c)
	assert.Equal(t, "MIT", c.license, "package.json fallback: %+v %v", c, err)
	assert.Contains(t, c.texts["package.json"], "Author: Ada", "package.json fallback: %+v %v", c, err)
	write("package.json", `{"license": "GPL-3.0"}`)
	_, err = readNpm("x", "1", dir)
	assert.Error(t, err, "GPL accepted")
	write("LICENSE", "GNU GENERAL PUBLIC LICENSE Version 3")
	_, err = readComponent("x", "1", dir)
	assert.Error(t, err, "an unaccepted license file accepted")
}

// Fonts bundled into the programs are read from their folders, each with
// its license, and printed in a section of their own.
func TestBundledFonts(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "LICENSE.txt"), []byte("This Font Software is licensed under the SIL Open Font License,\nVersion 1.1."), 0o644))
	fonts, problems := bundledFonts("Noto Sans=" + dir + "; ;Missing=" + filepath.Join(dir, "nope"))
	require.Len(t, fonts, 1)
	assert.Equal(t, "OFL-1.1", fonts[0].license)
	require.Len(t, problems, 1)
	assert.Contains(t, problems[0], "Missing")

	text := render(nil, nil, fonts, "APACHE")
	assert.Contains(t, text, "Fonts bundled into blitz")
	assert.Contains(t, text, "SIL Open Font License")
	assert.NotContains(t, render(nil, nil, nil, "APACHE"), "Fonts bundled")
}
