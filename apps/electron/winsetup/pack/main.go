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

// Command pack makes Blitz's Windows setup program: the setup program
// (winsetup, built for Windows) with the app's zip at its end, which it
// installs (spec_electron_038 EL-81). Run at build time, on any machine:
//
//	pack --exe winsetup.exe --zip Blitz_windows_x64.zip --out Blitz_setup.exe
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/retail-cortex/blitz/pkg/wininstall"
)

func main() { os.Exit(cmd(os.Args[1:], os.Stderr)) }

// cmd runs the command with its arguments, saying what went wrong on
// stderr; it returns the exit status.
func cmd(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("pack", flag.ContinueOnError)
	fs.SetOutput(stderr)
	exe := fs.String("exe", "", "the setup program")
	zipPath := fs.String("zip", "", "the app's zip")
	out := fs.String("out", "", "the setup program carrying the app")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := run(*exe, *zipPath, *out); err != nil {
		fmt.Fprintln(stderr, "pack:", err)
		return 1
	}
	return 0
}

func run(exe, zipPath, out string) error {
	if exe == "" || zipPath == "" || out == "" {
		return fmt.Errorf("--exe, --zip and --out are needed")
	}
	prog, err := os.ReadFile(exe)
	if err != nil {
		return err
	}
	z, err := os.ReadFile(zipPath)
	if err != nil {
		return err
	}
	var b bytes.Buffer
	b.Write(prog)
	if err := wininstall.AppendPayload(&b, z); err != nil {
		return err
	}
	return os.WriteFile(out, b.Bytes(), 0o755)
}
