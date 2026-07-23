// @ts-check

import assert from "node:assert/strict";
import test from "node:test";

import Ajv from "ajv";

import {
  createInputValidator,
  normalizeOpenAPISchema,
  normalizePythonPattern,
  validateURI,
} from "../pkg/cli/playground/lib/input/validation.js";

test("normalizes nullable schemas and Python patterns", () => {
  assert.deepEqual(normalizeOpenAPISchema({ type: "string", nullable: true }), {
    type: ["string", "null"],
  });
  assert.equal(
    normalizePythonPattern(String.raw`\A(?P<name>x)(?P=name)\Z`),
    String.raw`^(?<name>x)\k<name>$`,
  );
  assert.deepEqual(
    normalizeOpenAPISchema({ minimum: 1, exclusiveMinimum: true }),
    {
      minimum: 1,
      exclusiveMinimum: 1,
    },
  );
  assert.deepEqual(
    normalizeOpenAPISchema({ maximum: 2, exclusiveMaximum: false }),
    {
      maximum: 2,
    },
  );
  assert.deepEqual(normalizeOpenAPISchema({ exclusiveMinimum: true }), {});
});

test("validates standard and data URIs", () => {
  assert.equal(validateURI("https://example.com/a"), true);
  assert.equal(validateURI("data:text/plain;base64,aGVsbG8="), true);
  assert.equal(validateURI("data:text/plain;base64,%%%"), false);
});

test("returns field-oriented schema issues and required-empty errors", () => {
  const input = {
    type: "object",
    required: ["text"],
    properties: { text: { type: "string", pattern: "^ok" } },
    additionalProperties: false,
  };
  const validate = createInputValidator(Ajv, {}, input);
  assert.deepEqual(
    validate({ text: "" }).map((issue) => issue.message),
    ["Does not match the required pattern."],
  );
  assert.deepEqual(validate({ text: "no" })[0], {
    field: "text",
    keyword: "pattern",
    message: "Does not match the required pattern.",
    path: "text",
  });
  assert.deepEqual(
    validate({ text: "ok", extra: 1 })[0].message,
    "This field is not allowed.",
  );
  assert.deepEqual(validate({ text: "okay" }), []);
});

test("validates OpenAPI 3.0 exclusive numeric bounds", () => {
  const validate = createInputValidator(
    Ajv,
    {},
    {
      type: "number",
      minimum: 1,
      exclusiveMinimum: true,
    },
  );
  assert.equal(validate(1)[0]?.keyword, "exclusiveMinimum");
  assert.deepEqual(validate(2), []);
});

test("AJV validates referenced Cog inputs, formats, unions, arrays, and numeric constraints", () => {
  const document = {
    components: {
      schemas: {
        Prompt: {
          type: "string",
          minLength: 3,
          pattern: String.raw`\A[a-z]+\Z`,
        },
      },
    },
  };
  const input = {
    type: "object",
    required: ["prompt", "source", "count", "tags", "choice"],
    additionalProperties: false,
    properties: {
      prompt: { $ref: "#/components/schemas/Prompt" },
      source: { type: "string", format: "uri" },
      count: { type: "integer", minimum: 1, maximum: 4 },
      tags: {
        type: "array",
        minItems: 1,
        uniqueItems: true,
        items: { type: "string" },
      },
      choice: {
        oneOf: [
          { type: "string", enum: ["fast"] },
          { type: "integer", const: 2 },
        ],
      },
      mask: { type: "string", nullable: true },
    },
  };
  const validate = createInputValidator(
    Ajv,
    /** @type {import("../pkg/cli/playground/types").OpenAPIDocument} */ (
      document
    ),
    /** @type {import("../pkg/cli/playground/types").OpenAPISchema} */ (input),
  );

  assert.deepEqual(
    validate({
      prompt: "valid",
      source: "data:image/png;base64,YQ==",
      count: 2,
      tags: ["a", "b"],
      choice: "fast",
      mask: null,
    }),
    [],
  );
  const issues = validate({
    prompt: "NO",
    source: "not a uri",
    count: 5.5,
    tags: ["a", "a"],
    choice: false,
    extra: true,
  });
  assert.deepEqual(
    new Set(issues.map((issue) => issue.keyword)),
    new Set([
      "additionalProperties",
      "minLength",
      "pattern",
      "format",
      "type",
      "maximum",
      "uniqueItems",
      "oneOf",
    ]),
  );
  assert.ok(issues.every((issue) => issue.path && issue.message));
});

test("AJV schema compilation failures become validation issues", () => {
  const validate = createInputValidator(
    Ajv,
    {},
    {
      type: "string",
      minLength: /** @type {any} */ ("invalid"),
    },
  );
  const issue = validate("value")[0];
  assert.equal(issue?.keyword, "schema");
  assert.equal(issue?.path, "input");
  assert.match(issue?.message ?? "", /Cannot validate this OpenAPI schema/);
});
