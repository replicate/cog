// @ts-check

import { emptyInputValue } from "./input.js";

const REF_PREFIX = "#/components/schemas/";

/** @param {import("../../types").OpenAPIDocument} root @param {import("../../types").OpenAPISchema} value */
export function resolveRef(root, value) {
  if (!value.$ref?.startsWith(REF_PREFIX)) return value;
  return root.components?.schemas?.[value.$ref.slice(REF_PREFIX.length)] ?? value;
}

/** @param {import("../../types").OpenAPIDocument} root @param {import("../../types").OpenAPISchema} value */
export function effectiveSchema(root, value) {
  const schema = resolveRef(root, value);
  if (!schema.anyOf) return schema;
  const nonNull = schema.anyOf
    .map((item) => resolveRef(root, item))
    .filter((item) => !hasOnlyType(item, "null"));
  if (nonNull.length !== 1) return schema;
  const merged = { ...schema, ...nonNull[0] };
  delete merged.anyOf;
  return merged;
}

/** @param {import("../../types").OpenAPIDocument} root @param {import("../../types").OpenAPISchema} value */
export function enumValues(root, value) {
  const schema = effectiveSchema(root, value);
  if (schema.enum) return schema.enum;
  for (const item of schema.allOf ?? []) {
    const resolved = resolveRef(root, item);
    if (resolved.enum) return resolved.enum;
  }
  return undefined;
}

/** @param {import("../../types").OpenAPIDocument} root @param {import("../../types").OpenAPISchema} schema */
export function defaultInput(root, schema) {
  /** @type {import("../../types").JsonObject} */
  const result = {};
  const required = new Set(schema.required ?? []);
  for (const [name, raw] of orderedProperties(schema)) {
    const property = effectiveSchema(root, raw);
    const isRequired = required.has(name);
    if (property.default !== undefined) {
      if (isRequired || !property.default) result[name] = property.default;
    } else if (isRequired) {
      const choices = enumValues(root, property);
      if (choices?.length) result[name] = choices[0];
      else if (hasType(property, "boolean")) result[name] = false;
    } else {
      result[name] = enumValues(root, property)?.[0] ?? emptyInputValue(property);
    }
  }
  return result;
}

/** @param {import("../../types").OpenAPISchema} schema @returns {[string, import("../../types").OpenAPISchema][]} */
export function orderedProperties(schema) {
  return Object.entries(schema.properties ?? {}).sort(
    ([, left], [, right]) =>
      (typeof left["x-order"] === "number" ? left["x-order"] : 999) -
      (typeof right["x-order"] === "number" ? right["x-order"] : 999),
  );
}

/** @param {import("../../types").OpenAPISchema} schema */
export function constraintText(schema) {
  const parts = [];
  if (Object.hasOwn(schema, "default"))
    parts.push(
      `default ${typeof schema.default === "string" ? schema.default : JSON.stringify(schema.default)}`,
    );
  if (schema.minimum !== undefined) parts.push(`min ${schema.minimum}`);
  if (schema.maximum !== undefined) parts.push(`max ${schema.maximum}`);
  if (schema.minLength !== undefined) parts.push(`min ${schema.minLength} chars`);
  if (schema.maxLength !== undefined) parts.push(`max ${schema.maxLength} chars`);
  if (schema.pattern) parts.push(`pattern: ${schema.pattern}`);
  return parts.join(" · ");
}

/** @param {import("../../types").OpenAPIDocument} document @returns {import("../../types").PlaygroundCapabilities} */
export function playgroundCapabilities(document) {
  const schemas = document.components?.schemas ?? {};
  const training = Boolean(document.paths?.["/trainings"]);
  const endpoint = training ? "/trainings" : "/predictions";
  const input = resolveRef(document, schemas[training ? "TrainingInput" : "Input"] ?? {});
  return {
    endpoint,
    input,
    streaming: document.paths?.[endpoint]?.post?.["x-cog-streaming"] === true,
    async: Boolean(
      document.paths?.[`${endpoint}/{${training ? "training" : "prediction"}_id}/cancel`],
    ),
  };
}

/** @param {import("../../types").OpenAPISchema} schema @param {string} type */
export function hasType(schema, type) {
  return schema.type === type || (Array.isArray(schema.type) && schema.type.includes(type));
}
/** @param {import("../../types").OpenAPISchema} schema @param {string} type */
function hasOnlyType(schema, type) {
  return (
    schema.type === type ||
    (Array.isArray(schema.type) && schema.type.length === 1 && schema.type[0] === type)
  );
}
