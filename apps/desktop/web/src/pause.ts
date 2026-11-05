/**
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

// Waiting between a stream's tries.

/**
 * Resolves after ms, or at once when signal aborts; the abort listener
 * goes either way, so a loop that waits again and again on one signal
 * doesn't gather them.
 */
export function pause(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise<void>((res) => {
    if (signal.aborted) return res();
    const done = () => {
      clearTimeout(timer);
      signal.removeEventListener("abort", done);
      res();
    };
    const timer = setTimeout(done, ms);
    signal.addEventListener("abort", done);
  });
}
