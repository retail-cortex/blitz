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

package printpdf

func start(string, [2]float64, uintptr) error { return ErrUnsupported }

// InitUI reports false: there's no engine to print with here.
func InitUI() bool { return false }

// RunMainLoop returns at once: there's no UI loop to run.
func RunMainLoop() {}
