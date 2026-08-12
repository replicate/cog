import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    environment: "jsdom",
    include: ["tests/**/*.test.ts"],
    coverage: {
      provider: "v8",
      exclude: ["src/types.ts", "src/app.ts", "src/components/**", "src/worker/**"],
      include: ["src/**/*.ts"],
      thresholds: { branches: 40, functions: 40, lines: 40, statements: 40 },
    },
  },
});
