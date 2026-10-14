/*
 * Copyright 2026 Retail Cortex
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// The C side of gpu_probe (gpu_probe.c).

#ifndef BLITZ_GPU_PROBE_H
#define BLITZ_GPU_PROBE_H

// gpu_probe prints WebKit's webkit://gpu report for a web view drawing
// with the graphics card where it can (on != 0) or never; 0 when it read
// the report.
int gpu_probe(int on);

#endif
