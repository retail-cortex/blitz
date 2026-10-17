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

import "syscall"

// detached is how a program the tray starts runs: in a process group of
// its own, so it outlives the tray, with a console that's never shown.
// Not without a console (DETACHED_PROCESS): the service runs console
// programs all the time (git, shells, language servers), and Windows
// gives each one of a process without a console a window of its own,
// which flashed open and shut every few seconds. With a hidden console
// they share it. (A GUI program, such as Edge, ignores the flag.)
func detached() *syscall.SysProcAttr {
	const createNoWindow = 0x08000000
	return &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | createNoWindow, HideWindow: true}
}
