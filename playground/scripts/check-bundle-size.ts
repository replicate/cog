import { readdir, stat } from "node:fs/promises";

const assets = new URL("../../pkg/cli/playground/assets/", import.meta.url);
const maxJavaScriptBytes = 1024 * 1024;
const files = (await readdir(assets)).filter((file) => file.endsWith(".js"));
if (files.length === 0) throw new Error("No built playground JavaScript assets found");
const sizes = await Promise.all(
  files.map(async (file) => (await stat(new URL(file, assets))).size),
);
const total = sizes.reduce((sum, size) => sum + size, 0);

console.log(`Playground JavaScript: ${(total / 1024).toFixed(1)} KiB`);
if (total > maxJavaScriptBytes) {
  throw new Error(
    `Playground JavaScript exceeds the ${maxJavaScriptBytes / 1024 / 1024} MiB JavaScript budget`,
  );
}
