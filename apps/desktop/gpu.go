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

package main

import (
	"strings"

	"github.com/wailsapp/wails/v2/pkg/options/linux"
)

// gpuPolicy is how the window draws on Linux (DSK-100): with the graphics
// card where WebKitGTK can use it (its on-demand policy), unless the gpu
// preference is "off" or BLITZ_GPU says so. Off draws in software, as
// Wails does by default for drivers that crash it or leave the window
// blank; BLITZ_GPU=off is the way out when such a driver keeps the app
// from starting, and BLITZ_GPU=on overrides a saved "off".
func gpuPolicy(pref string, getenv func(string) string) linux.WebviewGpuPolicy {
	switch strings.ToLower(strings.TrimSpace(getenv("BLITZ_GPU"))) {
	case "off", "0", "false", "no":
		return linux.WebviewGpuPolicyNever
	case "on", "1", "true", "yes":
		return linux.WebviewGpuPolicyOnDemand
	}
	if pref == "off" {
		return linux.WebviewGpuPolicyNever
	}
	return linux.WebviewGpuPolicyOnDemand
}

// programName is the Linux window's class, which the dock or taskbar
// matches to the app's launcher (StartupWMClass in
// packaging/linux/blitz-desktop.desktop), whatever the binary is called.
const programName = "blitz-desktop"

// linuxOptions are the Linux window's options: the GPU as gpuPolicy has
// it, the window's class and its icon (for a taskbar that doesn't find the
// launcher's). Drawing in software also turns off WebKitGTK's DMA-BUF
// renderer (setenv, before WebKitGTK starts), the usual cause of a blank
// or garbled window on NVIDIA's drivers and in VMs, unless the
// environment already says.
func linuxOptions(pref string, getenv func(string) string, setenv func(k, v string) error) *linux.Options {
	policy := gpuPolicy(pref, getenv)
	if policy == linux.WebviewGpuPolicyNever && getenv("WEBKIT_DISABLE_DMABUF_RENDERER") == "" {
		_ = setenv("WEBKIT_DISABLE_DMABUF_RENDERER", "1")
	}
	return &linux.Options{WebviewGpuPolicy: policy, ProgramName: programName, Icon: windowIcon}
}
