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

package printpdf

// GTK and WebKitGTK's flags come from the BUILD file (@webkitgtk).

/*
#include <stdlib.h>
#include "print.h"
*/
import "C"

import "unsafe"

func start(html string, size [2]float64, h uintptr) error {
	page := C.CString(html)
	defer C.free(unsafe.Pointer(page))
	C.blitz_print_pdf(page, C.double(size[0]), C.double(size[1]), C.double(margin), C.uintptr_t(h))
	return nil
}

// InitUI readies GTK for a program without Wails, such as this package's
// tests, on the main thread; false where there's no display.
func InitUI() bool { return C.blitz_print_init() != 0 }

// RunMainLoop runs GTK's main loop on the calling thread, which must be
// the main one, after InitUI, and never returns.
func RunMainLoop() { C.blitz_print_run_loop() }
