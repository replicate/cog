import { isJsonObject, type JsonObject, type JsonValue } from "@/types/json";

export type OpenAPISchema = {
  $ref?: string;
  type?: string | string[];
  format?: string;
  title?: string;
  description?: string;
  default?: JsonValue;
  enum?: JsonValue[];
  allOf?: OpenAPISchema[];
  anyOf?: OpenAPISchema[];
  oneOf?: OpenAPISchema[];
  items?: boolean | OpenAPISchema;
  properties?: Record<string, OpenAPISchema>;
  additionalProperties?: boolean | OpenAPISchema;
  required?: string[];
  nullable?: boolean;
  minimum?: number;
  maximum?: number;
  exclusiveMinimum?: boolean | number;
  exclusiveMaximum?: boolean | number;
  multipleOf?: number;
  minLength?: number;
  maxLength?: number;
  pattern?: string;
  minItems?: number;
  maxItems?: number;
  uniqueItems?: boolean;
  minProperties?: number;
  maxProperties?: number;
  deprecated?: boolean;
  [key: `x-${string}`]: JsonValue;
};

export type OpenAPIDocument = {
  components?: { schemas?: Record<string, OpenAPISchema> };
  paths?: Record<string, OpenAPIPathItem>;
};

export type OpenAPIOperation = JsonObject & { "x-cog-streaming"?: boolean };
export type OpenAPIPathItem = JsonObject & { post?: OpenAPIOperation };

const MAX_SCHEMA_DEPTH = 64;
const MAX_SCHEMA_NODES = 10_000;

type SchemaValidationState = { remainingNodes: number };

/** Validates the subset of an OpenAPI document that the playground reads. */
export function isOpenAPIDocument(value: unknown): value is OpenAPIDocument {
  const state = { remainingNodes: MAX_SCHEMA_NODES };
  return (
    isJsonObject(value) &&
    isOptionalOpenAPIComponents(value.components, state) &&
    isOptionalRecord(value.paths, isOpenAPIPathItem)
  );
}

function isOptionalOpenAPIComponents(value: unknown, state: SchemaValidationState): boolean {
  return (
    value === undefined ||
    (isJsonObject(value) &&
      isOptionalRecord(value.schemas, (schema) => isOpenAPISchema(schema, state)))
  );
}

function isOpenAPIPathItem(value: unknown): value is OpenAPIPathItem {
  return isJsonObject(value) && (value.post === undefined || isOpenAPIOperation(value.post));
}

function isOpenAPIOperation(value: unknown): value is OpenAPIOperation {
  return isJsonObject(value) && isOptionalBoolean(value["x-cog-streaming"]);
}

function isOpenAPISchema(
  value: unknown,
  state: SchemaValidationState,
  depth = 0,
): value is OpenAPISchema {
  return (
    depth <= MAX_SCHEMA_DEPTH &&
    state.remainingNodes-- > 0 &&
    isJsonObject(value) &&
    ["$ref", "format", "title", "description", "pattern"].every((key) =>
      isOptionalString(value[key]),
    ) &&
    isOptionalSchemaType(value.type) &&
    [
      "minimum",
      "maximum",
      "multipleOf",
      "minLength",
      "maxLength",
      "minItems",
      "maxItems",
      "minProperties",
      "maxProperties",
    ].every((key) => isOptionalNumber(value[key])) &&
    ["exclusiveMinimum", "exclusiveMaximum"].every((key) =>
      isOptionalBooleanOrNumber(value[key]),
    ) &&
    ["nullable", "uniqueItems", "deprecated"].every((key) => isOptionalBoolean(value[key])) &&
    isOptionalArray(value.enum) &&
    isOptionalStringArray(value.required) &&
    ["allOf", "anyOf", "oneOf"].every((key) =>
      isOptionalArrayOf(value[key], (schema) => isOpenAPISchema(schema, state, depth + 1)),
    ) &&
    (value.items === undefined ||
      typeof value.items === "boolean" ||
      isOpenAPISchema(value.items, state, depth + 1)) &&
    isOptionalRecord(value.properties, (schema) => isOpenAPISchema(schema, state, depth + 1)) &&
    (value.additionalProperties === undefined ||
      typeof value.additionalProperties === "boolean" ||
      isOpenAPISchema(value.additionalProperties, state, depth + 1))
  );
}

function isOptionalString(value: unknown): boolean {
  return value === undefined || typeof value === "string";
}

function isOptionalSchemaType(value: unknown): boolean {
  return (
    isOptionalString(value) ||
    (Array.isArray(value) && value.every((item) => typeof item === "string"))
  );
}

function isOptionalNumber(value: unknown): boolean {
  return value === undefined || typeof value === "number";
}

function isOptionalBoolean(value: unknown): boolean {
  return value === undefined || typeof value === "boolean";
}

function isOptionalBooleanOrNumber(value: unknown): boolean {
  return value === undefined || typeof value === "boolean" || typeof value === "number";
}

function isOptionalArray(value: unknown): boolean {
  return value === undefined || Array.isArray(value);
}

function isOptionalStringArray(value: unknown): boolean {
  return (
    value === undefined || (Array.isArray(value) && value.every((item) => typeof item === "string"))
  );
}

function isOptionalArrayOf(value: unknown, validate: (value: unknown) => boolean): boolean {
  return value === undefined || (Array.isArray(value) && value.every(validate));
}

function isOptionalRecord(value: unknown, validate: (value: unknown) => boolean): boolean {
  return value === undefined || isRecord(value, validate);
}

function isRecord(value: unknown, validate: (value: unknown) => boolean): boolean {
  return isJsonObject(value) && Object.values(value).every(validate);
}
