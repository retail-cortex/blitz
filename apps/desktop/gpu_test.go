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
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
)

// The GPU is on unless the preference or BLITZ_GPU turns it off; the
// variable wins either way.
func TestGPUPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, pref, env string
		want            linux.WebviewGpuPolicy
	}{
		{"the default", "on", "", linux.WebviewGpuPolicyOnDemand},
		{"turned off", "off", "", linux.WebviewGpuPolicyNever},
		{"never saved", "", "", linux.WebviewGpuPolicyOnDemand},
		{"BLITZ_GPU=off", "on", "off", linux.WebviewGpuPolicyNever},
		{"BLITZ_GPU=0", "on", " 0 ", linux.WebviewGpuPolicyNever},
		{"BLITZ_GPU=on over a saved off", "off", "ON", linux.WebviewGpuPolicyOnDemand},
		{"BLITZ_GPU unreadable", "off", "maybe", linux.WebviewGpuPolicyNever},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := func(k string) string {
				if k == "BLITZ_GPU" {
					return tc.env
				}
				return ""
			}
			assert.Equal(t, tc.want, gpuPolicy(tc.pref, env))
		})
	}
}

// hasNvidia checks whether the NVIDIA control device exists.
func TestHasNvidia(t *testing.T) {
	assert.True(t, hasNvidia(t.TempDir()), "present device path")
	assert.False(t, hasNvidia(filepath.Join(t.TempDir(), "nvidiactl")), "absent device path")
}

// The Linux window: its class and icon; on NVIDIA with the GPU on,
// WebKitGTK's DMA-BUF renderer forced on with shared-memory handoff to
// GTK3; in software, the DMA-BUF renderer off; in each case, unless the
// environment already says.
func TestLinuxOptions(t *testing.T) {
	gpuNvidia := map[string]string{"WEBKIT_FORCE_DMABUF_RENDERER": "1", "WEBKIT_DMABUF_RENDERER_FORCE_SHM": "1"}
	for _, tc := range []struct {
		name, pref string
		nvidia     bool
		env        map[string]string
		want       linux.WebviewGpuPolicy
		set        map[string]string
	}{
		{"the GPU", "on", false, nil, linux.WebviewGpuPolicyOnDemand, map[string]string{}},
		{"the GPU on NVIDIA", "on", true, nil, linux.WebviewGpuPolicyOnDemand, gpuNvidia},
		{"the GPU on NVIDIA, SHM already set", "on", true, map[string]string{"WEBKIT_DMABUF_RENDERER_FORCE_SHM": "0"}, linux.WebviewGpuPolicyOnDemand,
			map[string]string{"WEBKIT_FORCE_DMABUF_RENDERER": "1"}},
		{"the GPU on NVIDIA, forced already", "on", true, map[string]string{"WEBKIT_FORCE_DMABUF_RENDERER": "1"}, linux.WebviewGpuPolicyOnDemand,
			map[string]string{"WEBKIT_DMABUF_RENDERER_FORCE_SHM": "1"}},
		{"the GPU on NVIDIA, DMA-BUF turned off", "on", true, map[string]string{"WEBKIT_DISABLE_DMABUF_RENDERER": "1"}, linux.WebviewGpuPolicyOnDemand,
			map[string]string{}},
		{"BLITZ_GPU=on on NVIDIA over a saved off", "off", true, map[string]string{"BLITZ_GPU": "on"}, linux.WebviewGpuPolicyOnDemand, gpuNvidia},
		{"software", "off", false, nil, linux.WebviewGpuPolicyNever, map[string]string{"WEBKIT_DISABLE_DMABUF_RENDERER": "1"}},
		{"software on NVIDIA", "off", true, nil, linux.WebviewGpuPolicyNever, map[string]string{"WEBKIT_DISABLE_DMABUF_RENDERER": "1"}},
		{"software, DMA-BUF already set", "off", false, map[string]string{"WEBKIT_DISABLE_DMABUF_RENDERER": "0"}, linux.WebviewGpuPolicyNever,
			map[string]string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			set := map[string]string{}
			env := func(k string) string { return tc.env[k] }
			o := linuxOptions(tc.pref, tc.nvidia, env, func(k, v string) error { set[k] = v; return nil })
			assert.Equal(t, tc.want, o.WebviewGpuPolicy)
			assert.Equal(t, tc.set, set)
			assert.Equal(t, "blitz-desktop", o.ProgramName, "the launcher's StartupWMClass")
			assert.Equal(t, windowIcon, o.Icon)
		})
	}
}
