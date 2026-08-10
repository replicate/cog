import assert from "node:assert/strict";
import { test } from "vitest";

import {
  constraintText,
  defaultInput,
  effectiveSchema,
  enumValues,
  initialInputValue,
  orderedProperties,
  playgroundCapabilities,
} from "../src/lib/input/openapi.js";

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

test("checks optional fields only when they have no declared default", () => {
  const schema = {
    required: ["enabled", "choice", "requiredZero", "requiredNull"],
    properties: {
      late: { type: "string" },
      enabled: { type: "boolean", "x-order": 1 },
      choice: { enum: [1, "1"], "x-order": 2 },
      optional: { type: "string", default: "yes" },
      optionalFalse: { type: "boolean", default: false },
      optionalZero: { type: "number", default: 0 },
      optionalEmpty: { type: "string", default: "" },
      optionalNull: { type: ["string", "null"], default: null },
      requiredZero: { type: "number", default: 0 },
      requiredNull: { type: ["string", "null"], default: null },
    },
  };
  assert.deepEqual(
    orderedProperties(schema).map(([name]) => name),
    [
      "enabled",
      "choice",
      "late",
      "optional",
      "optionalFalse",
      "optionalZero",
      "optionalEmpty",
      "optionalNull",
      "requiredZero",
      "requiredNull",
    ],
  );
  assert.deepEqual(defaultInput({}, schema), {
    enabled: false,
    choice: 1,
    late: "",
    requiredZero: 0,
    requiredNull: null,
  });
});

test("preserves null defaults when optional fields are enabled", () => {
  assert.equal(initialInputValue({}, { type: ["string", "null"], default: null }), null);
});

test("initializes optional fields without defaults by control type", () => {
  assert.deepEqual(
    defaultInput(
      {},
      {
        properties: {
          choice: { enum: [2, 3] },
          enabled: { type: "boolean" },
          items: { type: "array" },
          config: { type: "object" },
          count: { type: "number" },
        },
      },
    ),
    { choice: 2, enabled: false, items: [], config: {}, count: "" },
  );
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
