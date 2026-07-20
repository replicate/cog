// @ts-check

import assert from "node:assert/strict";
import test from "node:test";

import {
  constraintText,
  defaultInput,
  effectiveSchema,
  enumValues,
  orderedProperties,
  playgroundCapabilities,
} from "../pkg/cli/playground/lib/input/openapi.js";

test("resolves nullable references and preserves parent metadata", () => {
  const document = { components: { schemas: { Choice: { type: "string", enum: ["a", "b"] } } } };
  assert.deepEqual(
    effectiveSchema(document, {
      title: "choice",
      anyOf: [{ $ref: "#/components/schemas/Choice" }, { type: "null" }],
    }),
    { title: "choice", type: "string", enum: ["a", "b"] },
  );
  assert.deepEqual(enumValues(document, { allOf: [{ $ref: "#/components/schemas/Choice" }] }), [
    "a",
    "b",
  ]);
});

test("builds defaults and orders inputs without losing typed values", () => {
  const schema = {
    required: ["enabled", "choice"],
    properties: {
      late: { type: "string" },
      enabled: { type: "boolean", "x-order": 1 },
      choice: { enum: [1, "1"], "x-order": 2 },
      optional: { type: "string", default: "yes" },
    },
  };
  assert.deepEqual(
    orderedProperties(schema).map(([name]) => name),
    ["enabled", "choice", "late", "optional"],
  );
  assert.deepEqual(defaultInput({}, schema), { enabled: false, choice: 1, optional: "yes" });
});

test("derives training and prediction capabilities", () => {
  assert.deepEqual(
    playgroundCapabilities({
      components: { schemas: { Input: { type: "object" } } },
      paths: {
        "/predictions": { post: { "x-cog-streaming": true } },
        "/predictions/{prediction_id}/cancel": {},
      },
    }),
    { endpoint: "/predictions", input: { type: "object" }, streaming: true, async: true },
  );
  assert.equal(
    playgroundCapabilities({
      components: { schemas: { TrainingInput: { type: "object" } } },
      paths: { "/trainings": { post: {} } },
    }).endpoint,
    "/trainings",
  );
});

test("formats supported constraints", () => {
  assert.equal(
    constraintText({
      default: false,
      minimum: 1,
      maximum: 3,
      minLength: 2,
      maxLength: 4,
      pattern: "x+",
    }),
    "default false · min 1 · max 3 · min 2 chars · max 4 chars · pattern: x+",
  );
});
