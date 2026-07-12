import { type ChildProcess, spawn } from "node:child_process";
import { createServer } from "node:net";
import process from "node:process";
import { fileURLToPath } from "node:url";

const cog = process.env.COG_BINARY;
if (!cog) throw new Error("COG_BINARY must point to the built Cog CLI");
const cogBinary = cog;

const fixture = fileURLToPath(new URL("./fixture/", import.meta.url));
const modelPort = await availablePort();
const modelURL = `http://127.0.0.1:${modelPort}`;
const playgroundPort = 8400;
const playgroundURL = `http://127.0.0.1:${playgroundPort}`;
const children: ChildProcess[] = [];
const childFailures: Promise<never>[] = [];
let stopping = false;

function availablePort(): Promise<number> {
  return new Promise<number>((resolve, reject) => {
    const server = createServer();
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const address = server.address();
      server.close((error) => {
        if (error) reject(error);
        else if (typeof address === "object" && address) resolve(address.port);
        else reject(new Error("Could not allocate a model port"));
      });
    });
  });
}

function start(name: string, args: string[], cwd: string): Promise<never> {
  const child = spawn(cogBinary, args, {
    cwd,
    env: { ...process.env, BUILDKIT_PROGRESS: "quiet" },
    stdio: ["ignore", "pipe", "pipe"],
  });
  child.stdout?.on("data", (data: Buffer) => process.stdout.write(`[${name}] ${data}`));
  child.stderr?.on("data", (data: Buffer) => process.stderr.write(`[${name}] ${data}`));
  const failure = new Promise<never>((_, reject) => {
    child.once("error", reject);
    child.once("exit", (code, signal) => {
      if (!stopping) reject(new Error(`${name} exited unexpectedly (${code ?? signal})`));
    });
  });
  children.push(child);
  childFailures.push(failure);
  return failure;
}

async function waitFor(url: string, timeout: number): Promise<void> {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(url, { signal: AbortSignal.timeout(2000) });
      if (response.ok) return;
    } catch {
      // The process is still starting.
    }
    await new Promise((resolve) => setTimeout(resolve, 500));
  }
  throw new Error(`Timed out waiting for ${url}`);
}

async function waitForModel(timeout: number): Promise<void> {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(`${modelURL}/health-check`, {
        signal: AbortSignal.timeout(2000),
      });
      if (response.ok) {
        const health = (await response.json()) as { status?: string };
        if (health.status === "READY" || health.status === "BUSY") return;
        if (health.status === "SETUP_FAILED" || health.status === "DEFUNCT") {
          throw new Error(`Model startup failed with status ${health.status}`);
        }
      }
    } catch (error) {
      if (error instanceof Error && error.message.startsWith("Model startup failed")) throw error;
    }
    await new Promise((resolve) => setTimeout(resolve, 500));
  }
  throw new Error(`Timed out waiting for ${modelURL}`);
}

async function stop(): Promise<void> {
  if (stopping) return;
  stopping = true;
  try {
    await fetch(`${modelURL}/shutdown`, {
      method: "POST",
      signal: AbortSignal.timeout(5000),
    });
  } catch {
    // Fall back to process signals below.
  }
  for (const child of children) child.kill("SIGTERM");
  await new Promise((resolve) => setTimeout(resolve, 3000));
  for (const child of children) {
    if (child.exitCode === null) child.kill("SIGKILL");
  }
}

for (const signal of ["SIGINT", "SIGTERM"] as const) {
  process.once(signal, () => {
    void stop().finally(() => process.exit(0));
  });
}

try {
  // Enable Cog's host-gateway mapping so webhooks reach the host on Linux CI.
  const modelFailure = start(
    "model",
    ["serve", "--port", String(modelPort), "--upload-url", "http://unused/"],
    fixture,
  );
  await Promise.race([waitForModel(12 * 60_000), modelFailure]);
  const playgroundFailure = start(
    "playground",
    [
      "playground",
      "--host",
      "0.0.0.0",
      "--port",
      String(playgroundPort),
      "--target",
      modelURL,
      "--no-open",
    ],
    fixture,
  );
  await Promise.race([waitFor(playgroundURL, 30_000), playgroundFailure]);
  await Promise.race(childFailures);
} catch (error) {
  console.error(error);
  await stop();
  process.exit(1);
}
