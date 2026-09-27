import { readFileSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { resolve } from "node:path";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

const outDir = "../../cmd/blitz-desktop/dist";

// The desktop binary embeds the build, and go:embed only reaches files
// under its own package, so the build goes there. Emptying it removes the
// committed placeholder that lets the Go package compile before any build,
// so it's written back.
const wails = JSON.parse(readFileSync(resolve(import.meta.dirname, "../../cmd/blitz-desktop/wails.json"), "utf8"));

export default defineConfig({
  define: { __APP_VERSION__: JSON.stringify(wails.info.productVersion) },
  plugins: [
    react(),
    {
      name: "keep-placeholder",
      closeBundle() {
        writeFileSync(resolve(import.meta.dirname, outDir, ".gitkeep"), "");
      },
    },
  ],
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
