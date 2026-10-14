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

// Command gpu_probe asks WebKitGTK how it draws: a web view with the
// policy the desktop app gives it (gpuPolicy in ../gpu.go: "on" is on
// demand, "off" never) loads webkit://gpu, and the page's text goes to
// stdout. WebKit decides for the whole process (the display, the driver,
// the environment), so the caller sets the environment the app would
// (WEBKIT_DISABLE_DMABUF_RENDERER for "off"). gpu_check.sh reads it.
//
//	gpu_probe on|off
package main

// GTK and WebKitGTK's flags come from the BUILD file (@webkitgtk).

/*
#include "gpu_probe.h"
*/
import "C"

import (
	"fmt"
	"os"
	"runtime"
)

// GTK runs on the main thread.
func init() { runtime.LockOSThread() }

func main() {
	if len(os.Args) != 2 || (os.Args[1] != "on" && os.Args[1] != "off") {
		fmt.Fprintln(os.Stderr, "usage: gpu_probe on|off")
		os.Exit(2)
	}
	on := 0
	if os.Args[1] == "on" {
		on = 1
	}
	os.Exit(int(C.gpu_probe(C.int(on))))
}
