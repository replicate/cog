// @ts-check

import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import test from "node:test";

test("vendored browser assets match their recorded digests", async () => {
  const lock = JSON.parse(
    await readFile(new URL("../pkg/cli/playground/vendor/manifest.json", import.meta.url), "utf8"),
  );

  for (const [name, metadata] of Object.entries(lock)) {
    assert.equal(typeof metadata, "object");
    assert.ok(metadata);
    const expected = /** @type {{sha256?:unknown}} */ (metadata).sha256;
    assert.equal(typeof expected, "string", `${name} must record a SHA-256 digest`);
    const contents = await readFile(
      new URL(`../pkg/cli/playground/vendor/${name}`, import.meta.url),
    );
    const actual = createHash("sha256").update(contents).digest("hex");
    assert.equal(actual, expected, `${name} does not match manifest.json`);
  }
});
