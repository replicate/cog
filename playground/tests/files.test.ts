import assert from "node:assert/strict";
import { test } from "vitest";

import { fileToDataURI, FileReadGuard, MAX_LOCAL_FILE_SIZE } from "../src/lib/input/files.js";
import { dataMediaKind, emptyInputValue, formatBytes, isDataURI } from "../src/lib/input/input.js";

test("rejects local files larger than 16 MiB before reading", async () => {
  await assert.rejects(
    fileToDataURI(testFile(MAX_LOCAL_FILE_SIZE + 1)),
    /Local files must be 16 MiB or smaller/,
  );
});

test("resolves a successfully read file data URI", async () => {
  await withFileReader({ result: "data:image/png;base64,YQ==" }, async () => {
    assert.equal(await fileToDataURI(testFile(1)), "data:image/png;base64,YQ==");
  });
});

test("propagates browser file read errors", async () => {
  const failure = new Error("disk failed");
  await withFileReader({ error: failure }, async () => {
    await assert.rejects(fileToDataURI(testFile(1)), failure);
  });
});

test("rejects non-string FileReader results", async () => {
  await withFileReader({ result: new ArrayBuffer(1) }, async () => {
    await assert.rejects(fileToDataURI(testFile(1)), /Could not read file/);
  });
});

test("recognizes data URIs without misclassifying HTTP URLs", () => {
  assert.equal(isDataURI("data:text/plain,hello"), true);
  assert.equal(isDataURI("DATA:image/png;base64,YQ=="), true);
  assert.equal(isDataURI("https://example.com/data:image/png"), false);
});

test("classifies image, audio, and video previews", () => {
  assert.equal(dataMediaKind("data:image/png;base64,YQ=="), "image");
  assert.equal(dataMediaKind("data:audio/wav;base64,YQ=="), "audio");
  assert.equal(dataMediaKind("data:video/mp4;base64,YQ=="), "video");
  assert.equal(dataMediaKind("data:application/pdf;base64,YQ=="), undefined);
});

test("invalidates stale file reads after replacement or URL edits", () => {
  const reads = new FileReadGuard();
  const first = reads.begin();
  const second = reads.begin();
  assert.equal(reads.isCurrent(first), false);
  assert.equal(reads.isCurrent(second), true);
  reads.cancel();
  assert.equal(reads.isCurrent(second), false);
});

test("formats byte, KiB, and MiB file sizes", () => {
  assert.equal(formatBytes(100), "100 B");
  assert.equal(formatBytes(1536), "1.5 KB");
  assert.equal(formatBytes(2 * 1024 * 1024), "2.0 MB");
});

test("uses the original empty values for supported input controls", () => {
  assert.deepEqual(emptyInputValue({ type: "array" }), []);
  assert.deepEqual(emptyInputValue({ type: "object" }), {});
  assert.equal(emptyInputValue({ type: "boolean" }), false);
  assert.equal(emptyInputValue({ type: ["boolean", "null"] }), "");
  assert.equal(emptyInputValue({ type: "string" }), "");
});

type FileReaderOutcome = { result?: string | ArrayBuffer; error?: Error };

function testFile(size: number): File {
  return new File([new Uint8Array(size)], "input.bin");
}

async function withFileReader(outcome: FileReaderOutcome, run: () => Promise<void>): Promise<void> {
  const descriptor = Object.getOwnPropertyDescriptor(globalThis, "FileReader");
  class FakeFileReader {
    result = outcome.result ?? null;
    error = outcome.error ?? null;
    onload: ((event: Event) => void) | null = null;
    onerror: ((event: Event) => void) | null = null;
    readAsDataURL(): void {
      queueMicrotask(() => {
        const handler = this.error ? this.onerror : this.onload;
        handler?.(new Event(this.error ? "error" : "load"));
      });
    }
  }
  Object.defineProperty(globalThis, "FileReader", {
    configurable: true,
    value: FakeFileReader,
  });
  try {
    await run();
  } finally {
    if (descriptor) Object.defineProperty(globalThis, "FileReader", descriptor);
    else Reflect.deleteProperty(globalThis, "FileReader");
  }
}
