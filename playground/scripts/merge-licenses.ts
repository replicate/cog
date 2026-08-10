import { existsSync, readFileSync, readdirSync, unlinkSync, writeFileSync } from "node:fs";

type PackageMetadata = {
  dependencies?: { [name: string]: string };
  license?: string;
  version?: string;
};

function readLicenseSections(path: URL): Map<string, string> {
  const sections = new Map<string, string>();
  const source = readFileSync(path, "utf8").trimEnd();

  for (const section of source.split(/(?=^## )/m).slice(1)) {
    const name = /^## (.+?) - /m.exec(section)?.[1];
    if (!name) throw new Error(`Invalid license section in ${path.pathname}`);
    sections.set(name, section.trim());
  }
  return sections;
}

function licenseSection(name: string): string {
  const root = new URL(`../node_modules/${name}/`, import.meta.url);
  const metadata = readPackageMetadata(new URL("package.json", root));
  const licenseFile = readdirSync(root).find((file) => /^licen[cs]e(?:\..+)?$/i.test(file));

  if (!metadata.license || !metadata.version || !licenseFile)
    throw new Error(`Missing license attribution for ${name}`);

  const license = readFileSync(new URL(licenseFile, root), "utf8").trim();
  return `## ${name} - ${metadata.version} (${metadata.license})\n\n${license}`;
}

function readPackageMetadata(path: URL): PackageMetadata {
  return JSON.parse(readFileSync(path, "utf8")) as PackageMetadata;
}

function compareNames(left: string, right: string): number {
  if (left === right) return 0;
  return left < right ? -1 : 1;
}

const outputDirectory = new URL("../../pkg/cli/playground/", import.meta.url);
const mainLicensePath = new URL("THIRD_PARTY_LICENSES.md", outputDirectory);
const workerLicensePath = new URL("WORKER_LICENSES.md", outputDirectory);
const packagePath = new URL("../package.json", import.meta.url);

if (!existsSync(workerLicensePath)) throw new Error("Missing worker license output");

const sections = readLicenseSections(mainLicensePath);
for (const [name, section] of readLicenseSections(workerLicensePath)) {
  sections.set(name, section);
}

const dependencies = readPackageMetadata(packagePath).dependencies;
if (!dependencies) throw new Error("Playground package has no dependencies");

for (const dependency of Object.keys(dependencies)) {
  if (!sections.has(dependency)) sections.set(dependency, licenseSection(dependency));
}

const merged = [...sections.entries()]
  .sort(([left], [right]) => compareNames(left, right))
  .map(([, section]) => section)
  .join("\n\n");

writeFileSync(mainLicensePath, `# Third-party licenses\n\n${merged}\n`);
unlinkSync(workerLicensePath);
