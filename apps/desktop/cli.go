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

import "github.com/retail-cortex/blitz/pkg/pagehost"

// CLIStatus says where the blitz command is (pkg/pagehost).
type CLIStatus = pagehost.CLIStatus

// CLIInstall is what InstallCLI did (pkg/pagehost).
type CLIInstall = pagehost.CLIInstall

// CLIStatus reports where this app's blitz is and whether a terminal
// finds it.
func (a *App) CLIStatus() CLIStatus { return pagehost.CurrentCLIStatus() }

// InstallCLI puts this app's blitz on the user's PATH.
func (a *App) InstallCLI() (CLIInstall, error) { return pagehost.InstallCLI() }

// ReinstallCLI links this app's blitz again, removing dead links to old
// builds.
func (a *App) ReinstallCLI() (CLIInstall, error) { return pagehost.ReinstallCLI() }
