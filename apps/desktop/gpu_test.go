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

// The Linux window: its class and icon, and in software WebKitGTK's
// DMA-BUF renderer off too, unless the environment already says.
func TestLinuxOptions(t *testing.T) {
	for _, tc := range []struct {
		name, pref, dmabuf string
		want               linux.WebviewGpuPolicy
		set                map[string]string
	}{
		{"the GPU", "on", "", linux.WebviewGpuPolicyOnDemand, map[string]string{}},
		{"software", "off", "", linux.WebviewGpuPolicyNever, map[string]string{"WEBKIT_DISABLE_DMABUF_RENDERER": "1"}},
		{"software, DMA-BUF already set", "off", "0", linux.WebviewGpuPolicyNever, map[string]string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			set := map[string]string{}
			env := func(k string) string {
				if k == "WEBKIT_DISABLE_DMABUF_RENDERER" {
					return tc.dmabuf
				}
				return ""
			}
			o := linuxOptions(tc.pref, env, func(k, v string) error { set[k] = v; return nil })
			assert.Equal(t, tc.want, o.WebviewGpuPolicy)
			assert.Equal(t, tc.set, set)
			assert.Equal(t, "blitz-desktop", o.ProgramName, "the launcher's StartupWMClass")
			assert.Equal(t, windowIcon, o.Icon)
		})
	}
}
