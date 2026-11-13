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

//go:build unix

package ostrash

import (
	"os"
	"syscall"
)

// sameDevice reports whether a and b are on one device (a rename between
// them works).
func sameDevice(a, b string) bool {
	ia, errA := os.Lstat(a)
	ib, errB := os.Stat(b)
	if errA != nil || errB != nil {
		return false
	}
	sa, okA := ia.Sys().(*syscall.Stat_t)
	sb, okB := ib.Sys().(*syscall.Stat_t)
	return okA && okB && sa.Dev == sb.Dev
}
