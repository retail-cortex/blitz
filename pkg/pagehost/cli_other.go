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

//go:build !windows

package pagehost

// The registry is Windows's (cli_winpath.go): elsewhere there's none.
func registryUserPath() (string, error)   { return "", errNotWindows }
func registrySystemPath() (string, error) { return "", errNotWindows }
func registrySetUserPath(string) error    { return errNotWindows }
func announcePathChange()                 {}
