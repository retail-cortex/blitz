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

//go:build darwin || linux

package printpdf

/*
#include <stdint.h>
*/
import "C"

import (
	"errors"
	"unsafe"
)

// blitzPrintDone is how the engine's code answers: the PDF's bytes, or why
// there are none.
//
//export blitzPrintDone
func blitzPrintDone(handle C.uintptr_t, data unsafe.Pointer, n C.int, msg *C.char) {
	if msg != nil {
		done(uintptr(handle), nil, errors.New(C.GoString(msg)))
		return
	}
	done(uintptr(handle), C.GoBytes(data, n), nil)
}
