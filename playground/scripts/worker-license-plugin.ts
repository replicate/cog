import { readFileSync, readdirSync } from "node:fs";

import type { Plugin } from "vite";

type PackageLicense = {
  license: string;
  name: string;
  text: string;
  version: string;
};

/** Creates a Vite plugin that emits license notices for dependencies bundled into workers. */
export function workerLicensePlugin(): Plugin {
  return {
    name: "worker-license-file",
    /** Scans emitted worker chunks and writes a sorted Markdown license asset. */
    generateBundle(_options, bundle) {
      const packages = new Map<string, PackageLicense>();
      for (const output of Object.values(bundle)) {
        if (output.type !== "chunk") continue;
        for (const moduleId of Object.keys(output.modules)) {
          const packageRoot = packageRootForModule(moduleId);
          if (!packageRoot || packages.has(packageRoot)) continue;
          const license = readPackageLicense(packageRoot);
          if (license) packages.set(packageRoot, license);
        }
      }
      const sections = [...packages.values()]
        .sort((left, right) => left.name.localeCompare(right.name))
        .map(
          ({ license, name, text, version }) =>
            `## ${name} - ${version} (${license})\n\n${text.trim()}\n`,
        );
      this.emitFile({
        type: "asset",
        fileName: "WORKER_LICENSES.md",
        source: `# Worker licenses\n\n${sections.join("\n")}`,
      });
    },
  };
}

function packageRootForModule(moduleId: string): string | undefined {
  const cleanId = moduleId.split("?", 1)[0];
  const marker = "/node_modules/";
  const start = cleanId.lastIndexOf(marker);
  if (start < 0) return undefined;
  const packagePath = cleanId.slice(start + marker.length).split("/");
  const name = packagePath[0].startsWith("@")
    ? `${packagePath[0]}/${packagePath[1]}`
    : packagePath[0];
  return cleanId.slice(0, start + marker.length) + name;
}

function readPackageLicense(root: string): PackageLicense | undefined {
  try {
    const metadata = JSON.parse(readFileSync(`${root}/package.json`, "utf8")) as {
      license?: string;
      name?: string;
      version?: string;
    };
    if (!metadata.name || !metadata.version) return undefined;
    const licenseFile = readdirSync(root).find((name) => /^licen[cs]e(?:\..+)?$/i.test(name));
    if (!licenseFile) return undefined;
    return {
      license: metadata.license ?? "Unknown",
      name: metadata.name,
      text: readFileSync(`${root}/${licenseFile}`, "utf8"),
      version: metadata.version,
    };
  } catch {
    return undefined;
  }
}
