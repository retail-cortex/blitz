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
	"os"
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

// hasNvidia reports whether the NVIDIA driver's control device (dev,
// "/dev/nvidiactl") is present.
func hasNvidia(dev string) bool {
	_, err := os.Stat(dev)
	return err == nil
}

// programName is the Linux window's class, which the dock or taskbar
// matches to the app's launcher (StartupWMClass in
// packaging/linux/blitz-desktop.desktop), whatever the binary is called.
const programName = "blitz-desktop"

// linuxOptions are the Linux window's options: the GPU as gpuPolicy has
// it, the window's class and its icon (for a taskbar that doesn't find the
// launcher's). On NVIDIA with the GPU on, WebKitGTK's DMA-BUF renderer
// hands frames to the GTK3 window in shared memory
// (WEBKIT_DMABUF_RENDERER_FORCE_SHM=1) so the web process draws on the GPU
// without attaching a Wayland explicit-sync object
// (wp_linux_drm_syncobj_surface_v1) to GTK3's wl_shm surface, which the
// compositor rejects with protocol error 71. Drawing in software turns off
// the DMA-BUF renderer altogether, unless the environment already says.
func linuxOptions(pref string, nvidia bool, getenv func(string) string, setenv func(k, v string) error) *linux.Options {
	policy := gpuPolicy(pref, getenv)
	switch {
	case policy == linux.WebviewGpuPolicyNever && getenv("WEBKIT_DISABLE_DMABUF_RENDERER") == "":
		_ = setenv("WEBKIT_DISABLE_DMABUF_RENDERER", "1")
	case policy == linux.WebviewGpuPolicyOnDemand && nvidia &&
		getenv("WEBKIT_DMABUF_RENDERER_FORCE_SHM") == "" && getenv("WEBKIT_DISABLE_DMABUF_RENDERER") == "":
		_ = setenv("WEBKIT_DMABUF_RENDERER_FORCE_SHM", "1")
	}
	return &linux.Options{WebviewGpuPolicy: policy, ProgramName: programName, Icon: windowIcon}
}
