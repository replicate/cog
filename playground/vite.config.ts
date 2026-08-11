import path from "node:path";
import { fileURLToPath } from "node:url";

import { defineConfig, type Plugin } from "vite";

import { workerLicensePlugin } from "./scripts/worker-license-plugin.js";

const root = path.dirname(fileURLToPath(import.meta.url));

export default defineConfig({
  build: {
    emptyOutDir: true,
    license: { fileName: "THIRD_PARTY_LICENSES.md" },
    outDir: path.resolve(root, "../pkg/cli/playground"),
    sourcemap: false,
  },
  resolve: {
    alias: { "@": path.resolve(root, "src") },
  },
  worker: {
    plugins: (): Plugin[] => [workerLicensePlugin()],
    rollupOptions: {
      output: { entryFileNames: "assets/validation.worker-[hash].js" },
    },
  },
});
