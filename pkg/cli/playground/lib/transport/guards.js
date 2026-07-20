// @ts-check

import { isJsonObject } from "../json.js";

const MAX_SCHEMA_DEPTH = 64;
const MAX_SCHEMA_NODES = 10_000;

/** @param {unknown} value @returns {value is import("../../types").HealthResponse} */
export function isHealthResponse(value) {
  return (
    isJsonObject(value) &&
    optionalString(value.status) &&
    optionalString(value.user_healthcheck_error) &&
    (value.setup === undefined ||
      (isJsonObject(value.setup) &&
        optionalString(value.setup.status) &&
        optionalString(value.setup.logs))) &&
    (value.version === undefined ||
      (isJsonObject(value.version) &&
        optionalString(value.version.coglet) &&
        optionalString(value.version.python_sdk) &&
        optionalString(value.version.python)))
  );
}

/** @param {unknown} value @returns {value is import("../../types").OpenAPIDocument} */
export function isOpenAPIDocument(value) {
  const state = { remaining: MAX_SCHEMA_NODES };
  return (
    isJsonObject(value) &&
    (value.components === undefined ||
      (isJsonObject(value.components) &&
        optionalRecord(value.components.schemas, (schema) => isSchema(schema, state, 0)))) &&
    optionalRecord(
      value.paths,
      (item) =>
        isJsonObject(item) &&
        (item.post === undefined ||
          (isJsonObject(item.post) && optionalBoolean(item.post["x-cog-streaming"]))),
    )
  );
}

/** @param {unknown} value @returns {import("../../types").PredictionEnvelope | undefined} */
export function predictionEnvelope(value) {
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

/** @param {unknown} value */
function optionalString(value) {
  return value === undefined || typeof value === "string";
}
/** @param {unknown} value */
function optionalNumber(value) {
  return value === undefined || typeof value === "number";
}
/** @param {unknown} value */
function optionalBoolean(value) {
  return value === undefined || typeof value === "boolean";
}
/** @param {unknown} value */
function optionalBooleanOrNumber(value) {
  return value === undefined || typeof value === "boolean" || typeof value === "number";
}
/** @param {unknown} value */
function optionalStringArray(value) {
  return (
    value === undefined || (Array.isArray(value) && value.every((item) => typeof item === "string"))
  );
}
/** @param {unknown} value @param {(item: unknown) => boolean} validate */
function optionalRecord(value, validate) {
  return value === undefined || (isJsonObject(value) && Object.values(value).every(validate));
}

/** @param {unknown} value @param {{remaining: number}} state @param {number} depth @returns {boolean} */
function isSchema(value, state, depth) {
  if (depth > MAX_SCHEMA_DEPTH || state.remaining-- <= 0 || !isJsonObject(value)) return false;
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
  if (
    value.items !== undefined &&
    typeof value.items !== "boolean" &&
    !isSchema(value.items, state, depth + 1)
  )
    return false;
  if (!optionalRecord(value.properties, (child) => isSchema(child, state, depth + 1))) return false;
  return (
    value.additionalProperties === undefined ||
    typeof value.additionalProperties === "boolean" ||
    isSchema(value.additionalProperties, state, depth + 1)
  );
}
