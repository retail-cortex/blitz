import { readFileSync } from "node:fs";
import { homedir } from "node:os";
import { resolve } from "node:path";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Bazel builds the page (//apps/desktop/web:page) into its output tree,
// named by BLITZ_PAGE_OUT, and the desktop binary embeds it from there.
const outDir = process.env.BLITZ_PAGE_OUT ?? "dist";

const wails = JSON.parse(readFileSync(resolve(import.meta.dirname, "../wails.json"), "utf8"));

export default defineConfig({
  define: { __APP_VERSION__: JSON.stringify(wails.info.productVersion) },
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
