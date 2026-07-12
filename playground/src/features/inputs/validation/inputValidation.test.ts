import { describe, expect, it } from "vitest";

import type { OpenAPIDocument, OpenAPISchema } from "@/types/openapi";
import { createInputValidator } from "@/features/inputs/validation/inputValidation";

const document: OpenAPIDocument = {
  components: {
    schemas: {
      Choice: { type: "string", enum: ["red", "blue"] },
    },
  },
};

describe("createInputValidator", () => {
  it("validates OpenAPI types, constraints, formats, refs, and nested values", () => {
    const schema: OpenAPISchema = {
      type: "object",
      additionalProperties: false,
      required: ["name", "count", "choice", "url", "items", "config"],
      properties: {
        name: { type: "string", minLength: 2, maxLength: 5, pattern: "^[a-z]+$" },
        count: { type: "integer", minimum: 1, maximum: 3 },
        choice: { allOf: [{ $ref: "#/components/schemas/Choice" }] },
        url: { type: "string", format: "uri" },
        items: { type: "array", minItems: 1, items: { type: "number" } },
        config: {
          type: "object",
          required: ["enabled"],
          properties: { enabled: { type: "boolean" } },
        },
        note: { type: "string", nullable: true },
        variant: { anyOf: [{ type: "string" }, { type: "number" }], nullable: true },
      },
    };
    const validate = createInputValidator(document, schema);

    expect(
      validate({
        name: "model",
        count: 2,
        choice: "red",
        url: "https://example.com/input.png",
        items: [1, 2],
        config: { enabled: true },
        note: null,
        variant: null,
      }),
    ).toEqual([]);

    const issues = validate({
      count: 1.5,
      choice: "green",
      url: "not a uri",
      items: ["wrong"],
      config: {},
      variant: false,
      extra: true,
    });
    expect(issues).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ path: "name", keyword: "required" }),
        expect.objectContaining({ path: "count", keyword: "type" }),
        expect.objectContaining({ path: "choice", keyword: "enum" }),
        expect.objectContaining({ path: "url", keyword: "format" }),
        expect.objectContaining({ path: "items[0]", keyword: "type" }),
        expect.objectContaining({ path: "config.enabled", keyword: "required" }),
        expect.objectContaining({ path: "variant", keyword: "anyOf" }),
        expect.objectContaining({ path: "extra", keyword: "additionalProperties" }),
      ]),
    );
  });

  it("keeps additional properties when the OpenAPI schema allows them", () => {
    const validate = createInputValidator(document, {
      type: "object",
      properties: { prompt: { type: "string" } },
    });

    expect(validate({ prompt: "hello", custom: { nested: true } })).toEqual([]);
  });

  it("enforces the remaining scalar, array, object, and composition constraints", () => {
    const validate = createInputValidator(document, {
      type: "object",
      properties: {
        text: { type: "string", minLength: 2, maxLength: 4, pattern: "^[a-z]+$" },
        number: {
          type: "number",
          minimum: 1,
          maximum: 5,
          exclusiveMinimum: true,
          exclusiveMaximum: true,
          multipleOf: 0.5,
        },
        list: {
          type: "array",
          minItems: 2,
          maxItems: 3,
          uniqueItems: true,
          items: { type: "integer" },
        },
        object: {
          type: "object",
          minProperties: 1,
          maxProperties: 2,
          additionalProperties: { type: "string" },
        },
        exact: { oneOf: [{ type: "string" }, { type: "integer" }] },
      },
    });

    expect(
      validate({
        text: "test",
        number: 1.5,
        list: [1, 2],
        object: { custom: "value" },
        exact: "value",
      }),
    ).toEqual([]);
    expect(validate({ text: "A", number: 1, list: [1, 1, 2, 3], object: {} })).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ path: "text", keyword: "minLength" }),
        expect.objectContaining({ path: "text", keyword: "pattern" }),
        expect.objectContaining({ path: "number", keyword: "minimum" }),
        expect.objectContaining({ path: "list", keyword: "maxItems" }),
        expect.objectContaining({ path: "list", keyword: "uniqueItems" }),
        expect.objectContaining({ path: "object", keyword: "minProperties" }),
      ]),
    );
  });

  it("translates Python regex syntax that has a browser equivalent", () => {
    const validate = createInputValidator(document, {
      type: "object",
      properties: { value: { type: "string", pattern: "(?P<name>value)" } },
    });

    expect(validate({ value: "value" })).toEqual([]);

    const validateUnsupportedPattern = createInputValidator(document, {
      type: "object",
      properties: { value: { type: "string", pattern: "(?i)value" } },
    });
    expect(validateUnsupportedPattern({ value: "VALUE" })).toEqual([]);
  });

  it("does not normalize schema-like keys inside enum payloads", () => {
    const payload = { nullable: true, pattern: "(?i)literal" };
    const validate = createInputValidator(document, {
      type: "object",
      properties: { payload: { enum: [payload], default: payload } },
    });

    expect(validate({ payload })).toEqual([]);
    expect(validate({ payload: { nullable: false, pattern: "(?i)literal" } })).toEqual([
      expect.objectContaining({ path: "payload", keyword: "enum" }),
    ]);
  });

  it("validates large uploaded data URIs without overflowing the URI formatter", () => {
    const validate = createInputValidator(document, {
      type: "object",
      properties: { file: { type: "string", format: "uri" } },
    });
    const dataURI = `data:application/octet-stream;base64,${"A".repeat(9 * 1024 * 1024)}`;

    expect(validate({ file: dataURI })).toEqual([]);
    expect(validate({ file: "DATA:text/plain;base64,QQ%3D%3D" })).toEqual([]);
    expect(validate({ file: "not a uri" })).toEqual([
      expect.objectContaining({ path: "file", keyword: "format" }),
    ]);
    expect(validate({ file: "data:text/plain;base64,QQ%3D" })).toEqual([
      expect.objectContaining({ path: "file", keyword: "format" }),
    ]);
    expect(validate({ file: "data:text/plain;base64,QQ%ZZ" })).toEqual([
      expect.objectContaining({ path: "file", keyword: "format" }),
    ]);
  });
});
