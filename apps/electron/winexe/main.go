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

// Command winexe gives a Windows program Blitz's icon and details (the
// name Explorer, the taskbar and Task Manager show): it rewrites the
// resources of the Electron program the Windows zip ships as Blitz.exe
// (spec_electron_038 EL-06). Run at build time, on any machine:
//
//	winexe --in electron.exe --icon appicon.png --version 0.6.6 --out Blitz.exe
package main

import (
	"bytes"
	"flag"
	"fmt"
	"image"
	_ "image/png"
	"io"
	"os"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"
)

// iconSizes are the sizes Windows draws a program's icon at, from the
// taskbar's smallest to Explorer's largest.
var iconSizes = []int{16, 20, 24, 32, 40, 48, 64, 96, 128, 256}

func main() { os.Exit(cmd(os.Args[1:], os.Stderr)) }

// cmd runs the command with its arguments, saying what went wrong on
// stderr; it returns the exit status.
func cmd(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("winexe", flag.ContinueOnError)
	fs.SetOutput(stderr)
	in := fs.String("in", "", "the program to rewrite")
	icon := fs.String("icon", "", "its icon, a square PNG of 256 px or more")
	ver := fs.String("version", "0.0.0", "its version (a.b.c)")
	out := fs.String("out", "", "where the rewritten program goes")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := run(*in, *icon, *ver, *out); err != nil {
		fmt.Fprintln(stderr, "winexe:", err)
		return 1
	}
	return 0
}

func run(in, iconPath, ver, out string) error {
	if in == "" || iconPath == "" || out == "" {
		return fmt.Errorf("--in, --icon and --out are needed")
	}
	exe, err := os.ReadFile(in)
	if err != nil {
		return err
	}
	png, err := os.ReadFile(iconPath)
	if err != nil {
		return err
	}
	var b bytes.Buffer
	if err := Rewrite(bytes.NewReader(exe), png, ver, &b); err != nil {
		return err
	}
	return os.WriteFile(out, b.Bytes(), 0o755)
}

// Rewrite writes exe with Blitz's icon (from png, at every size Windows
// uses) as its first icon and its version details naming Blitz, keeping
// its other resources (its manifest among them).
func Rewrite(exe io.ReadSeeker, png []byte, ver string, out io.Writer) error {
	rs, err := winres.LoadFromEXE(exe)
	if err != nil {
		return fmt.Errorf("reading the program's resources: %w", err)
	}
	img, _, err := image.Decode(bytes.NewReader(png))
	if err != nil {
		return fmt.Errorf("reading the icon: %w", err)
	}
	if b := img.Bounds(); b.Dx() != b.Dy() || b.Dx() < 256 {
		return fmt.Errorf("the icon must be square and at least 256 px, not %dx%d", b.Dx(), b.Dy())
	}
	icon, err := winres.NewIconFromResizedImage(img, iconSizes)
	if err != nil {
		return err
	}
	// Explorer shows the icon with the lowest ID; Electron's is 1.
	if err := rs.SetIcon(winres.ID(1), icon); err != nil {
		return err
	}

	var v version.Info
	v.SetFileVersion(ver)
	v.SetProductVersion(ver)
	for k, s := range map[string]string{
		version.ProductName:      "Blitz",
		version.FileDescription:  "Blitz",
		version.CompanyName:      "Retail Cortex",
		version.InternalName:     "Blitz",
		version.OriginalFilename: "Blitz.exe",
		version.LegalCopyright:   "Copyright 2026 Retail Cortex",
		version.ProductVersion:   ver,
		version.FileVersion:      ver,
	} {
		if err := v.Set(0, k, s); err != nil {
			return err
		}
	}
	rs.SetVersionInfo(v)

	if _, err := exe.Seek(0, io.SeekStart); err != nil {
		return err
	}
	// Electron's signature no longer matches once the resources change: the
	// program goes unsigned rather than wrongly signed.
	return rs.WriteToEXE(out, exe, winres.WithAuthenticode(winres.RemoveSignature))
}
