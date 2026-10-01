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
import { homedir } from "node:os";
import { dirname } from "node:path";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Bazel builds the page (//apps/desktop/web:page) into its output tree,
// named by BLITZ_PAGE_OUT, and the desktop binary embeds it from there.
const outDir = process.env.BLITZ_PAGE_OUT ?? "dist";

// The page's tests' coverage (//apps/desktop/web:coverage): lines and
// branches of src/, without the generated client, the browser-only
// stand-ins and the entry point. Bazel keeps the report among the test's
// outputs.
const coverageDir = process.env.TEST_UNDECLARED_OUTPUTS_DIR ? `${process.env.TEST_UNDECLARED_OUTPUTS_DIR}/coverage` : "coverage";

export default defineConfig({
  plugins: [react()],
  // Relative asset paths: the VS Code extension serves the page from a
  // folder of its own (apps/vscode), the desktop app from its root.
  base: "./",
  // The app loads its page from its own binary, so one bundle is fine.
  build: { outDir, emptyOutDir: true, chunkSizeWarningLimit: 1024 },
  test: {
    // Under Bazel the tests start in the runfiles tree, whose files are
    // links; Node runs (and reports coverage for) their real paths, in
    // the directory the real package.json is in.
    root: dirname(realpathSync("package.json")),
    coverage: {
      provider: "v8",
      include: ["src/**/*.{ts,tsx}"],
      exclude: ["src/gen/**", "src/dev/**", "src/**/*.test.{ts,tsx}", "src/main.tsx", "src/env.d.ts"],
      reporter: ["text-summary", "json-summary", "lcov"],
      reportsDirectory: coverageDir,
      // The floor, as tools/coverage/floor.txt is the Go code's: raise it
      // when coverage rises, never lower it to let a change through.
      thresholds: { lines: 17, branches: 16 },
    },
  },
  // In development (plain browser), API calls go to the service's socket,
  // as the desktop app's Go side does.
  server: {
    proxy: {
      "/blitz.v1.": {
        target: { socketPath: process.env.BLITZ_SOCKET || `${homedir()}/.blitz/run/blitz.sock` } as unknown as string,
      },
    },
  },
});
