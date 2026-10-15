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

// Package webprocess quits the Linux app with its web processes ended
// first (DSK-100). Closing the window destroys the web view and WebKitGTK's
// web process exits on its own; on NVIDIA, with the GPU on, the driver's
// EGL tears down in the exit handlers while WebKit's painting threads still
// release their GL contexts, and the web process dies of a segfault (a
// core dump and a crash report each time the window closes). Ended by
// WebKit first, it's killed, and no exit handler runs.
package webprocess
