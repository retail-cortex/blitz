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
	"bytes"
	"context"
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
		return nil, fmt.Errorf("bwrap probe failed (are user namespaces enabled?): %v: %s", err, bytes.TrimSpace(out))
	}
	scan := newBlockedScan(spec) // kept between commands
	return func(ctx context.Context, argv []string) ([]string, error) {
		files, dirs, err := scan.expandCtx(ctx) // a cold scan can take seconds
		if err != nil {
			return nil, err
		}
		return append(bwrapArgs(bwrap, spec, files, dirs), argv...), nil
	}, nil
}

// sandboxHint says how to get the sandbox, or do without it, when it's
// required and unavailable.
const sandboxHint = "On Ubuntu 24.04, AppArmor keeps bubblewrap from the user namespaces it needs: allow them " +
	"(sudo sysctl -w kernel.apparmor_restrict_unprivileged_userns=0, or an AppArmor profile for bwrap), " +
	"or set sandbox.shell = \"auto\" in ~/.blitz/.env.toml to run commands unsandboxed"
