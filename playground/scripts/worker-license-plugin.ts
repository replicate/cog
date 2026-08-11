import { readFileSync, readdirSync } from "node:fs";
import type { Plugin } from "vite";

type PackageLicense = { license: string; name: string; text: string; version: string };
type BundleOutput = { modules?: { [moduleId: string]: object }; type: string };
type BundleOutputs = { [fileName: string]: BundleOutput };

export function workerLicensePlugin(): Plugin {
  return {
    name: "worker-license-file",
    generateBundle(_options, bundle) {
      const packages = new Map<string, PackageLicense>();
      for (const output of Object.values(bundle as BundleOutputs)) {
        if (output.type !== "chunk") continue;
        for (const moduleId of Object.keys(output.modules ?? {})) {
          const root = packageRootForModule(moduleId);
          if (!root || packages.has(root)) continue;
          packages.set(root, readPackageLicense(root));
        }
      }
      const sections = [...packages.values()]
        .sort((left, right) => (left.name < right.name ? -1 : left.name > right.name ? 1 : 0))
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

function readPackageLicense(root: string): PackageLicense {
  const metadata = JSON.parse(
    readFileSync(`${root}/package.json`, "utf8"),
  ) as Partial<PackageLicense>;
  if (!metadata.name || !metadata.version || !metadata.license)
    throw new Error(`Missing package metadata for ${root}`);

  const licenseFile = readdirSync(root).find((name) => /^licen[cs]e(?:\..+)?$/i.test(name));
  if (!licenseFile) throw new Error(`Missing license text for ${metadata.name}`);

  return {
    license: metadata.license,
    name: metadata.name,
    text: readFileSync(`${root}/${licenseFile}`, "utf8"),
    version: metadata.version,
  };
}
