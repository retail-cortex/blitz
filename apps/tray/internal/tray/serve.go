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

package tray

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/retail-cortex/blitz/pkg/pageserver"
)

// Link is what Serve writes for each link it makes: one JSON line.
type Link struct {
	URL string `json:"url"`
	// Fingerprint is the page server's certificate's (SHA-256, colon-
	// separated hex): the one certificate the window trusts for it.
	Fingerprint string `json:"fingerprint"`
}

// Serve serves the desktop page for a window that shows it (the Electron
// app, which runs blitz-tray --serve): it writes a one-time link to the
// page to out as a JSON line, a new one for each "link" line it reads
// from in, and stops serving when in ends, so it never outlives the
// window that started it.
func Serve(o pageserver.Options, in io.Reader, out io.Writer) error {
	srv, err := pageserver.Start(o)
	if err != nil {
		return err
	}
	defer srv.Close()
	enc := json.NewEncoder(out)
	if err := enc.Encode(Link{URL: srv.OpenURL(), Fingerprint: srv.Fingerprint()}); err != nil {
		return err
	}
	lines := bufio.NewScanner(in)
	for lines.Scan() {
		if strings.TrimSpace(lines.Text()) == "link" {
			if err := enc.Encode(Link{URL: srv.OpenURL(), Fingerprint: srv.Fingerprint()}); err != nil {
				return err
			}
		}
	}
	return lines.Err()
}

// PageDir is the built page in dir, checked to be one (it has index.html).
func PageDir(dir string) (fs.FS, error) {
	page := os.DirFS(dir)
	if _, err := fs.Stat(page, "index.html"); err != nil {
		return nil, fmt.Errorf("%s isn't the desktop page (no index.html): %w", dir, err)
	}
	return page, nil
}
