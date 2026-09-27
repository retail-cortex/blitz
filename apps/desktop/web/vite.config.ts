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

import { homedir } from "node:os";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Bazel builds the page (//apps/desktop/web:page) into its output tree,
// named by BLITZ_PAGE_OUT, and the desktop binary embeds it from there.
const outDir = process.env.BLITZ_PAGE_OUT ?? "dist";

export default defineConfig({
  plugins: [react()],
  // The app loads its page from its own binary, so one bundle is fine.
  build: { outDir, emptyOutDir: true, chunkSizeWarningLimit: 1024 },
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
