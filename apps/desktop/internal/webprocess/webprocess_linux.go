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

package webprocess

// GTK and WebKitGTK's flags come from the BUILD file (@webkitgtk).

/*
#include <stdlib.h>
#include "webprocess.h"
*/
import "C"

import "unsafe"

// Quit ends the web process of every web view in the app's windows, then
// GTK's main loop, on GTK's thread: Wails then closes the window as it
// does after its own quit. It reports true, so the caller (OnBeforeClose)
// leaves quitting to it; it may be called from any goroutine.
func Quit() bool {
	C.blitz_quit_without_web_processes()
	return true
}

// The tests' hooks (cgo can't be used in a test file).

// initUI starts GTK on the calling thread, the main one; false where
// there's no display.
func initUI() bool { return C.blitz_webprocess_init() != 0 }

// runMainLoop runs GTK's main loop on the main thread, after initUI.
func runMainLoop() { C.blitz_webprocess_run_loop() }

// openView opens an offscreen window with a web view showing html.
func openView(html string) {
	s := C.CString(html)
	defer C.free(unsafe.Pointer(s))
	C.blitz_webprocess_open_view(s)
}

// viewLoaded reports whether openView's page has loaded.
func viewLoaded() bool { return C.blitz_webprocess_view_loaded() != 0 }

// viewEnded reports whether openView's web process has ended: 1 by
// WebKit's API, -1 otherwise (a crash), 0 not yet.
func viewEnded() int { return int(C.blitz_webprocess_view_ended()) }

// endWebProcesses ends every web view's web process, on GTK's thread,
// without quitting.
func endWebProcesses() { C.blitz_webprocess_end_on_loop() }
