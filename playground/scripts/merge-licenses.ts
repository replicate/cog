import { readFileSync, unlinkSync, writeFileSync } from "node:fs";

const outputDirectory = new URL("../../pkg/cli/playground/", import.meta.url);
const mainPath = new URL("THIRD_PARTY_LICENSES.md", outputDirectory);
const workerPath = new URL("WORKER_LICENSES.md", outputDirectory);
const main = readFileSync(mainPath, "utf8").trimEnd();
const worker = readFileSync(workerPath, "utf8");
const existing = new Set([...main.matchAll(/^## (.+?) - .+$/gm)].map((match) => match[1]));
const workerSections = worker
  .split(/(?=^## )/m)
  .slice(1)
  .filter((section) => {
    const name = /^## (.+?) - /m.exec(section)?.[1];
    return name && !existing.has(name);
  });
const merged = `${main}\n\n${workerSections.join("\n").trim()}\n`;

for (const dependency of ["ajv", "ajv-draft-04", "ajv-formats", "fast-uri"]) {
  if (!merged.includes(`## ${dependency} - `)) {
    throw new Error(`Worker dependency ${dependency} is missing from THIRD_PARTY_LICENSES.md`);
  }
}

writeFileSync(mainPath, merged);
unlinkSync(workerPath);
