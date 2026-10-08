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

import "path/filepath"

// appWindow is the command that opens link in a window of its own. On
// Windows that's Edge (every Windows has it) as an app: no tabs or address
// bar, so the page looks like the desktop app. Without Edge, and on other
// systems, it's the default browser.
func appWindow(goos string, getenv func(string) string, isFile func(string) bool, link string) (string, []string) {
	switch goos {
	case "windows":
		if edge := findEdge(getenv, isFile); edge != "" {
			return edge, []string{"--app=" + link}
		}
		return "rundll32", []string{"url.dll,FileProtocolHandler", link}
	case "darwin":
		return "open", []string{link}
	}
	return "xdg-open", []string{link}
}

// findEdge looks for msedge.exe where its installers put it: for every
// user, then for this one ("" when it isn't there).
func findEdge(getenv func(string) string, isFile func(string) bool) string {
	for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles", "LOCALAPPDATA"} {
		root := getenv(env)
		if root == "" {
			continue
		}
		if p := filepath.Join(root, "Microsoft", "Edge", "Application", "msedge.exe"); isFile(p) {
			return p
		}
	}
	return ""
}
