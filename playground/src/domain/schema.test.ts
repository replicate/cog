import { describe, expect, it } from "vitest";

import {
  constraintText,
  defaultInput,
  effectiveSchema,
  enumValues,
  inputAndOutputSchemas,
  orderedProperties,
} from "./schema";
import type { OpenAPIDocument } from "./types";

const document: OpenAPIDocument = {
  components: {
    schemas: {
      Choice: { enum: [1, 2] },
      Input: {
        required: ["prompt", "enabled", "choice"],
        properties: {
          late: { type: "string", "x-order": 2 },
          prompt: { type: "string", default: "hello", "x-order": 1 },
          optional: { type: "boolean", default: false },
          enabled: { type: "boolean" },
          choice: { allOf: [{ $ref: "#/components/schemas/Choice" }] },
        },
      },
      Output: { type: "string" },
    },
  },
  paths: {
    "/predictions": { post: { "x-cog-streaming": true } },
    "/predictions/{prediction_id}/cancel": {},
  },
};

describe("schema helpers", () => {
  it("resolves nullable references without losing metadata", () => {
    expect(
      effectiveSchema(document, {
        title: "Nullable choice",
        anyOf: [{ $ref: "#/components/schemas/Choice" }, { type: "null" }],
      }),
    ).toEqual({ title: "Nullable choice", enum: [1, 2] });
  });

  it("initializes explicit defaults and required controls", () => {
    const input = document.components?.schemas?.Input ?? {};
    expect(orderedProperties(input).map(([name]) => name)).toEqual([
      "prompt",
      "late",
      "optional",
      "enabled",
      "choice",
    ]);
    expect(defaultInput(document, input)).toEqual({
      prompt: "hello",
      optional: false,
      enabled: false,
      choice: 1,
    });
  });

  it("discovers enums from direct and composed schemas", () => {
    expect(enumValues(document, { $ref: "#/components/schemas/Choice" })).toEqual([1, 2]);
    expect(enumValues(document, { allOf: [{ $ref: "#/components/schemas/Choice" }] })).toEqual([
      1, 2,
    ]);
  });

  it("derives prediction capabilities", () => {
    expect(inputAndOutputSchemas(document)).toMatchObject({
      endpoint: "/predictions",
      streaming: true,
      async: true,
      input: document.components?.schemas?.Input,
    });
  });

  it("derives training capabilities", () => {
    const training: OpenAPIDocument = {
      components: {
        schemas: { TrainingInput: { type: "object" }, TrainingOutput: { type: "object" } },
      },
      paths: { "/trainings": { post: {} } },
    };
    expect(inputAndOutputSchemas(training)).toMatchObject({
      endpoint: "/trainings",
      streaming: false,
      async: false,
    });
  });

  it("formats constraints", () => {
    expect(
      constraintText({ minimum: 1, maximum: 9, minLength: 2, maxLength: 10, pattern: "^[a-z]+$" }),
    ).toBe("min 1 · max 9 · min 2 chars · max 10 chars · pattern: ^[a-z]+$");
  });
});
