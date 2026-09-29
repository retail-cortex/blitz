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

//go:build linux

package tools

import (
	"fmt"
	"os/exec"
)

// nativeSandbox uses bubblewrap (bwrap). It probes once, since bwrap needs
// unprivileged user namespaces, which some distributions and containers
// disable.
func nativeSandbox(spec OSSandboxSpec) (sandboxWrapper, error) {
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		return nil, fmt.Errorf("bubblewrap (bwrap) not found; install it (e.g. apt install bubblewrap)")
	}
	probe := bwrapArgs(bwrap, spec, nil, nil)
	probe = append(probe, "/bin/true")
	if out, err := exec.Command(probe[0], probe[1:]...).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("bwrap probe failed (are user namespaces enabled?): %v: %s", err, out)
	}
	scan := newBlockedScan(spec) // kept between commands
	return func(argv []string) []string {
		files, dirs := scan.expand()
		return append(bwrapArgs(bwrap, spec, files, dirs), argv...)
	}, nil
}
