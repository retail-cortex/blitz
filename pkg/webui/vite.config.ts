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

import { realpathSync } from "node:fs";
import { dirname } from "node:path";
import { defineConfig } from "vite";

// The package's own tests (//pkg/webui:test). Under Bazel they start in
// the runfiles tree, whose files are links: Node runs their real paths.
export default defineConfig({
  test: { root: dirname(realpathSync("package.json")) },
});
