import assert from "node:assert/strict";
import { test } from "vitest";

import { replaceChildrenPreservingFocus } from "../src/components/dom.js";

test("replaces children when nothing inside the root is focused", () => {
  const root = document.createElement("div");
  const child = document.createElement("section");

  replaceChildrenPreservingFocus(root, child);

  assert.equal(root.firstElementChild, child);
});
