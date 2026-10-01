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

//go:build !darwin && !linux

package tools

import (
	"fmt"
	"runtime"
)

// nativeSandbox: only macOS (Seatbelt) and Linux (bubblewrap) are supported.
func nativeSandbox(spec OSSandboxSpec) (sandboxWrapper, error) {
	return nil, fmt.Errorf("no OS sandbox implementation for %s", runtime.GOOS)
}

// sandboxHint says how to do without the sandbox when it's required and
// unavailable.
const sandboxHint = "Set sandbox.shell = \"auto\" in the settings to run commands unsandboxed"
