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
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/retail-cortex/blitz/pkg/legal"
	"golang.org/x/term"
)

// LicenseText is what `license` shows for which: "" (the NOTICE and where
// to find the rest), "full" (the Apache License) or "third-party" (the
// notices of the software Blitz includes). command is how the caller is
// invoked, for the pointers in the summary.
func LicenseText(which, command string) (string, error) {
	switch which {
	case "":
		return legal.Summary(command), nil
	case "full":
		return legal.License, nil
	case "third-party", "third_party", "thirdparty":
		return legal.ThirdParty, nil
	}
	return "", fmt.Errorf("unknown license text %q: use full or third-party", which)
}

// Page writes text to out, through $PAGER (else less) when out is a
// terminal and the text is taller than it.
func Page(out io.Writer, text string) error {
	f, ok := out.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		_, err := io.WriteString(out, text)
		return err
	}
	_, rows, err := term.GetSize(int(f.Fd()))
	if err != nil || strings.Count(text, "\n") < rows-1 {
		_, err := io.WriteString(out, text)
		return err
	}
	pager := os.Getenv("PAGER")
	if pager == "" {
		pager = "less"
	}
	cmd := exec.Command("sh", "-c", pager)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = strings.NewReader(text), f, os.Stderr
	if err := cmd.Run(); err != nil {
		// No pager: print it.
		_, err := io.WriteString(out, text)
		return err
	}
	return nil
}
