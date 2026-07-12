import path from "node:path";
import { fileURLToPath } from "node:url";

import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "vite";

import { workerLicensePlugin } from "./scripts/worker-license-plugin";

const root = path.dirname(fileURLToPath(import.meta.url));

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(root, "src"),
    },
  },
  build: {
    outDir: path.resolve(root, "../pkg/cli/playground"),
    emptyOutDir: true,
    license: { fileName: "THIRD_PARTY_LICENSES.md" },
  },
  worker: {
    /** Creates a fresh license collector for each worker build. */
    plugins: () => [workerLicensePlugin()],
    rollupOptions: {
      output: { entryFileNames: "assets/validation.worker-[hash].js" },
    },
  },
});
