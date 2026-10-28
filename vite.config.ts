import { hostname } from "node:os";
import { fileURLToPath } from "node:url";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// The dev launcher (cmd/dev) picks the API's port per worktree and passes it
// in. Without it, the frontend runs alone with nothing behind /api.
const apiURL = process.env.VITE_API_URL;

export default defineConfig({
  build: {
    // The Go binary embeds this directory (internal/webapp).
    emptyOutDir: true,
    outDir: "../internal/webapp/dist",
    // Stack traces from production point at the source.
    sourcemap: true,
  },
  clearScreen: false,
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { "@": fileURLToPath(new URL("./app", import.meta.url)) },
  },
  root: "app",
  server: {
    allowedHosts: [hostname()],
    host: true,
    // The browser's Host header is kept, so the API sees the frontend's
    // requests as same-origin and accepts them.
    proxy: apiURL
      ? {
          "/api": apiURL,
          "/health": apiURL,
        }
      : undefined,
    strictPort: true,
  },
});
