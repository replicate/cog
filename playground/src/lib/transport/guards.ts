import type {
  HealthResponse,
  OpenAPIDocument,
  OpenAPIPathItem,
  OpenAPISchema,
  PredictionEnvelope,
  StringMap,
} from "../../types.js";

import { isJsonObject, isObject } from "../json.js";

const MAX_SCHEMA_DEPTH = 64;
const MAX_SCHEMA_NODES = 10_000;

export function isHealthResponse(value: unknown): value is HealthResponse {
  if (!isObject(value)) return false;
  if (!optionalString(value.status) || !optionalString(value.user_healthcheck_error)) return false;
  return isOptionalSetup(value.setup) && isOptionalVersion(value.version);
}

export function isOpenAPIDocument(value: unknown): value is OpenAPIDocument {
  const state = { remaining: MAX_SCHEMA_NODES };
  return (
    isObject(value) &&
    (value.components === undefined ||
      (isObject(value.components) &&
        optionalRecord(value.components.schemas, (schema) => isSchema(schema, state, 0)))) &&
    optionalRecord(
      value.paths,
      (item): item is OpenAPIPathItem =>
        isObject(item) &&
        (item.post === undefined ||
          (isObject(item.post) && optionalBoolean(item.post["x-cog-streaming"]))),
    )
  );
}

export function isOpenAPISchema(value: unknown): value is OpenAPISchema {
  return isSchema(value, { remaining: MAX_SCHEMA_NODES }, 0);
}

export function predictionEnvelope(value: unknown): PredictionEnvelope | undefined {
  if (
    !isJsonObject(value) ||
    !["id", "status", "error", "logs"].every(
      (key) => value[key] === undefined || value[key] === null || typeof value[key] === "string",
    ) ||
    (value.metrics !== undefined && value.metrics !== null && !isJsonObject(value.metrics))
  )
    return undefined;
  const { error, id, logs, metrics, status, ...other } = value;
  return {
    ...other,
    ...(typeof id === "string" && { id }),
    ...(typeof status === "string" && { status }),
    ...(typeof error === "string" && { error }),
    ...(typeof logs === "string" && { logs }),
    ...(isJsonObject(metrics) && { metrics }),
  };
}

function optionalString(value: unknown): value is string | undefined {
  return value === undefined || typeof value === "string";
}

function isOptionalSetup(value: unknown): value is HealthResponse["setup"] {
  return (
    value === undefined ||
    (isObject(value) && optionalString(value.status) && optionalString(value.logs))
  );
}

function isOptionalVersion(value: unknown): value is HealthResponse["version"] {
  return (
    value === undefined ||
    (isObject(value) &&
      optionalString(value.coglet) &&
      optionalString(value.python_sdk) &&
      optionalString(value.python))
  );
}

function optionalNumber(value: unknown): value is number | undefined {
  return value === undefined || (typeof value === "number" && Number.isFinite(value));
}

function optionalBoolean(value: unknown): value is boolean | undefined {
  return value === undefined || typeof value === "boolean";
}

function optionalBooleanOrNumber(value: unknown): value is boolean | number | undefined {
  return value === undefined || typeof value === "boolean" || typeof value === "number";
}

function optionalStringArray(value: unknown): value is string[] | undefined {
  return (
    value === undefined || (Array.isArray(value) && value.every((item) => typeof item === "string"))
  );
}

function optionalRecord<T>(
  value: unknown,
  validate: (item: unknown) => item is T,
): value is StringMap<T> | undefined {
  return value === undefined || (isObject(value) && Object.values(value).every(validate));
}

function isSchema(
  value: unknown,
  state: { remaining: number },
  depth: number,
): value is OpenAPISchema {
  if (depth > MAX_SCHEMA_DEPTH) return false;
  if (state.remaining <= 0) return false;
  if (!isObject(value)) return false;
  state.remaining -= 1;

  if (
    !["$ref", "format", "title", "description", "pattern"].every((key) =>
      optionalString(value[key]),
    )
  )
    return false;
  if (
    !(
      optionalString(value.type) ||
      (Array.isArray(value.type) && value.type.every((item) => typeof item === "string"))
    )
  )
    return false;

  if (
    ![
      "minimum",
      "maximum",
      "multipleOf",
      "minLength",
      "maxLength",
      "minItems",
      "maxItems",
      "minProperties",
      "maxProperties",
    ].every((key) => optionalNumber(value[key]))
  )
    return false;

  if (!["exclusiveMinimum", "exclusiveMaximum"].every((key) => optionalBooleanOrNumber(value[key])))
    return false;
  if (!["nullable", "uniqueItems", "deprecated"].every((key) => optionalBoolean(value[key])))
    return false;
  if (value.enum !== undefined && !Array.isArray(value.enum)) return false;
  if (!optionalStringArray(value.required)) return false;

  for (const key of ["allOf", "anyOf", "oneOf"]) {
    if (
      value[key] !== undefined &&
      (!Array.isArray(value[key]) ||
        !value[key].every((child) => isSchema(child, state, depth + 1)))
    )
      return false;
  }
  if (value.items !== undefined) {
    if (Array.isArray(value.items)) {
      if (!value.items.every((child) => isSchema(child, state, depth + 1))) return false;
    } else if (typeof value.items !== "boolean" && !isSchema(value.items, state, depth + 1)) {
      return false;
    }
  }

  for (const keyword of ["properties", "patternProperties", "definitions"] as const)
    if (!optionalRecord(value[keyword], (child) => isSchema(child, state, depth + 1))) return false;
  for (const keyword of ["additionalItems", "not"] as const) {
    const child = value[keyword];
    if (child !== undefined && typeof child !== "boolean" && !isSchema(child, state, depth + 1))
      return false;
  }
  if (
    value.dependencies !== undefined &&
    (!isObject(value.dependencies) ||
      !Object.values(value.dependencies).every(
        (dependency) =>
          (Array.isArray(dependency) && dependency.every((item) => typeof item === "string")) ||
          isSchema(dependency, state, depth + 1),
      ))
  )
    return false;

  return (
    value.additionalProperties === undefined ||
    typeof value.additionalProperties === "boolean" ||
    isSchema(value.additionalProperties, state, depth + 1)
  );
}
